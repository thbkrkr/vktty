package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	env "github.com/kelseyhightower/envconfig"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	port = 8042
)

var recycleTick = time.NewTicker(10 * time.Second)

type Config struct {
	Lifetime             time.Duration
	PoolCapacity         int `envconfig:"POOL_CAPACITY"`
	PoolSize             int `envconfig:"POOL_SIZE"`
	PoolParallelCreation int `envconfig:"POOL_PARALLEL_CREATION"`

	Domain string
	Blurb  string `envconfig:"BLURB"`

	ScriptPath string `envconfig:"SCRIPT_PATH"`
}

func main() {
	var cfg Config
	err := env.Process("vktty", &cfg)
	if err != nil {
		log.Fatal("fail to process env:", err.Error())
	}

	logger := newLogger(cfg)
	logger.Info("Start",
		zap.String("env", Env(cfg)),
		zap.String("config", fmt.Sprintf("+%v", cfg)))

	pool := NewPool(cfg, logger)

	r := gin.Default()
	basicAuth := gin.BasicAuth(gin.Accounts{
		"admin": cfg.Blurb,
	})

	r.GET("/ls", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"vclusters": pool.Ls(),
		})
	})

	r.GET("/sudo/ls", basicAuth, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"vclusters": pool.SudoLs(),
		})
	})

	r.GET("/lock", func(c *gin.Context) {
		vcluster, err := pool.Get()
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"message": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"🐰": fmt.Sprintf("http://z:%s@%s:3132%d", vcluster.key, cfg.Domain, vcluster.ID),
		})
	})

	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "🐰",
		})
	})

	r.Run(fmt.Sprintf(":%d", port))
}

type Status string

var (
	Creating Status = "creating"
	Free     Status = "free"
	Locked   Status = "locked"
	Deleting Status = "deleting"
	Error    Status = "error"
)

type VCluster struct {
	ID       int
	Creation *time.Time `json:",omitempty"`
	Key      string     `json:",omitempty"`
	key      string
	Status   Status
}

type Pool struct {
	l         *zap.Logger
	config    Config
	vclusters []*VCluster
	mux       *sync.Mutex
}

func NewPool(c Config, l *zap.Logger) *Pool {
	p := &Pool{
		l:         l,
		config:    c,
		vclusters: make([]*VCluster, c.PoolSize),
		mux:       &sync.Mutex{},
	}

	p.Init(c)
	go p.Start()

	return p
}

func (p *Pool) Init(c Config) {
	for i := 0; i < c.PoolCapacity; i++ {
		p.mux.Lock()
		p.vclusters[i] = &VCluster{ID: i, Status: Creating}
		p.mux.Unlock()
		go p.Add(i)
	}
}

func (p *Pool) Ls() []VCluster {
	p.mux.Lock()
	vclusters := []VCluster{}
	for _, v := range p.vclusters {
		if v != nil {
			vclusters = append(vclusters, *v)
		}
	}
	p.mux.Unlock()

	return vclusters
}

func (p *Pool) SudoLs() []VCluster {
	vclusters := p.Ls()
	// expose sensitive data
	for i := range vclusters {
		vclusters[i].Key = vclusters[i].key
	}
	return vclusters
}

var (
	errCreating                = errors.New("please come back in a moment")
	errMaxPoolParallelCreation = errors.New("please come back later")
	errMaxCapacity             = errors.New("max capacity")
)

func (p *Pool) Get() (VCluster, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	creating := 0

	// look for a free vcluster
	for i, v := range p.vclusters {
		if v == nil {
			continue
		}
		if v.Status == Creating {
			creating++
		}
		if v.Status == Free {
			p.l.Info("Lock", zap.Int("id", v.ID))
			now := time.Now()
			p.vclusters[i].Creation = &now
			p.vclusters[i].Status = Locked
			return *v, nil
		}
	}
	// check if max creation is reached
	if creating < int(p.config.PoolParallelCreation) {
		return VCluster{}, errMaxPoolParallelCreation
	}

	// else create a cluster if there is space left
	for i, v := range p.vclusters {
		if v == nil {
			p.vclusters[i] = &VCluster{ID: i, Status: Creating}
			go p.Add(i)
			return VCluster{}, errCreating
		}
	}

	return VCluster{}, errMaxCapacity
}

func (p *Pool) Start() {
	for range recycleTick.C {
		p.mux.Lock()
		for i, v := range p.vclusters {
			if isEOL(v, p.config.Lifetime) {
				v.Status = Deleting
				v.Creation = nil
				p.vclusters[i] = v
				go p.Delete(i)
			}
		}
		p.mux.Unlock()
	}
}

func isEOL(v *VCluster, lifetime time.Duration) bool {
	return v != nil &&
		(v.Creation != nil && time.Now().After(v.Creation.Add(lifetime)) ||
			v.Status == Error)
}

func (p *Pool) Add(i int) {
	p.Exec(i, "create", func(v *VCluster, r execResult) *VCluster {
		v.key = r.Key
		v.Status = Free
		return v
	})
}

func (p *Pool) Delete(i int) {
	p.Exec(i, "delete", func(*VCluster, execResult) *VCluster {
		// no more
		return nil
	})
}

type execResult struct {
	Status int
	Key    string
}

func (p *Pool) Exec(i int, action string, callback func(*VCluster, execResult) *VCluster) {
	li := p.l.With(zap.Int("id", i), zap.String("action", action))
	li.Info("Start")

	cmd := exec.Command(p.config.ScriptPath, action, fmt.Sprintf("%d", i))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, stderr)
	err := cmd.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			p.Error(li, i, "Unexpected", zap.Error(err), stdout, stderr)
			return
		}
	}

	var res execResult
	err = json.Unmarshal(stdout.Bytes(), &res)
	if err != nil {
		p.Error(li, i, "Parsing", zap.Error(err), stdout, stderr)
		return
	}
	if res.Status != 0 {
		p.Error(li, i, "Error", zap.Int("status_code", res.Status), stdout, stderr)
		return
	}

	p.mux.Lock()
	p.vclusters[i] = callback(p.vclusters[i], res)
	p.mux.Unlock()

	li.Info("Succeeded")
}

func (p *Pool) Error(l *zap.Logger, i int, msg string, f zapcore.Field, stdout *bytes.Buffer, stderr *bytes.Buffer) {
	p.mux.Lock()
	p.vclusters[i] = &VCluster{ID: i, Status: Error}
	p.mux.Unlock()
	l.Error(msg,
		f,
		zap.String("stdout", stdout.String()),
		zap.String("stderr", stderr.String()),
	)
}

func newLogger(c Config) *zap.Logger {
	stdout := zapcore.AddSync(os.Stdout)
	level := zap.NewAtomicLevelAt(zap.InfoLevel)

	z := zap.NewProductionEncoderConfig()
	z.TimeKey = "timestamp"
	z.EncodeTime = zapcore.ISO8601TimeEncoder

	if isDev(c) {
		z = zap.NewDevelopmentEncoderConfig()
		z.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	return zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(z), stdout, level))
}

func Env(c Config) string {
	env := "prod"
	if isDev(c) {
		env = "dev"
	}
	return env
}

func isDev(c Config) bool {
	return c.Blurb == "dev"
}
