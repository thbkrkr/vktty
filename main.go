package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	env "github.com/kelseyhightower/envconfig"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	port = 8042
)

var (
	recycleTick = time.NewTicker(10 * time.Second)
	flushTick   = time.NewTicker(1 * time.Second)

	errCreating                = errors.New("please come back in a moment")
	errMaxPoolParallelCreation = errors.New("please come back later")
	errMaxCapacity             = errors.New("max capacity")

	statusCodes = map[error]int{
		errCreating:                http.StatusAccepted,
		errMaxPoolParallelCreation: 509, // Bandwidth Limit Exceeded
		errMaxCapacity:             http.StatusServiceUnavailable,
	}
)

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

	kcfg, err := rest.InClusterConfig()
	if err != nil {
		kcfg, err = clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), "/.kube/config"))
		if err != nil {
			log.Fatal("fail to create k8s client config", err.Error())
		}
	}
	k8sclient, err := kubernetes.NewForConfig(kcfg)
	if err != nil {
		log.Fatal("fail to create k8s client", err.Error())
	}

	pool := NewPool(cfg, logger, k8sclient)

	if isDev(cfg) {
		gin.SetMode(gin.ReleaseMode)
	}
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

	r.GET("/get", func(c *gin.Context) {
		vcluster, err := pool.Get()
		if err != nil {
			var s int
			var ok bool
			if s, ok = statusCodes[err]; !ok {
				s = http.StatusInternalServerError
			}
			c.JSON(s, gin.H{
				"msg": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"msg": fmt.Sprintf("http://z:%s@%s:3132%d", vcluster.key, cfg.Domain, vcluster.ID),
		})
	})

	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"msg": "🐰",
		})
	})

	r.GET("/info", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"parallel_creation": cfg.PoolParallelCreation,
			"capacity":          cfg.PoolCapacity,
			"size":              cfg.PoolSize,
			"lifetime":          cfg.Lifetime,
		})
	})

	r.Run(fmt.Sprintf(":%d", port))
}

type Status string

var (
	Creating Status = "Creating"
	Free     Status = "Free"
	Locked   Status = "Locked"
	Deleting Status = "Deleting"
	Error    Status = "Error"

	Running Status = "Running"
)

type VCluster struct {
	Name    string
	ID      int
	Created *time.Time `json:",omitempty"`
	Key     string     `json:",omitempty"`
	key     string
	Status  Status
}

type Pool struct {
	l         *zap.Logger
	config    Config
	vclusters []*VCluster
	mux       *sync.Mutex
	k         *kubernetes.Clientset
}

func NewPool(c Config, l *zap.Logger, k *kubernetes.Clientset) *Pool {
	p := &Pool{
		l:         l,
		config:    c,
		vclusters: make([]*VCluster, c.PoolSize),
		mux:       &sync.Mutex{},
		k:         k,
	}

	p.Sync(c)
	//go p.Flush()
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

func (p *Pool) Sync(c Config) {
	p.l.Info("Start sync")

	cmd := exec.Command("vcluster", "ls", "--output=json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		p.l.Error("Fail to list", zap.Error(err))
		return
	}

	var ls []VCluster
	err = json.Unmarshal(out, &ls)
	if err != nil {
		p.l.Error("Fail to parse list", zap.Error(err))
		return
	}

	p.l.Info("List", zap.String("list", fmt.Sprintf("+%v", ls)))
	for _, v := range ls {
		v := v
		i, err := strconv.Atoi(strings.Replace(v.Name, "c", "", -1))
		if err != nil {
			p.l.Error("Fail to parse name", zap.Error(err))
			return
		}
		v.ID = i
		prevStatus := v.Status
		v.Status = Error
		eol := p.isEOL(&v)
		if prevStatus == Running && !eol {
			v.Status = Locked
		}

		p.vclusters[i] = &v
		p.l.Info("Sync update", zap.Int("id", i),
			zap.String("status", string(v.Status)), zap.String("prevStatus", string(prevStatus)),
			zap.Bool("eol", eol))
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

func (p *Pool) Get() (VCluster, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	creating := 0

	// look for a free vcluster
	for i := 0; i < p.config.PoolSize; i++ {
		v := p.vclusters[i]
		if v == nil {
			continue
		}
		if v.Status == Creating {
			creating++
		}
		if v.Status == Free {
			p.l.Info("Lock", zap.Int("id", v.ID))
			now := time.Now()
			p.vclusters[i].Created = &now
			p.vclusters[i].Status = Locked
			return *v, nil
		}
	}
	// check if max creation is reached
	if creating >= int(p.config.PoolParallelCreation) {
		return VCluster{}, errMaxPoolParallelCreation
	}

	// else create a cluster if there is space left
	for i := 0; i < p.config.PoolSize; i++ {
		v := p.vclusters[i]
		if v == nil {
			// New
			p.vclusters[i] = &VCluster{Name: fmt.Sprintf("c%d", i), ID: i, Status: Creating}
			go p.Add(i)
			return VCluster{}, errCreating
		}
	}

	return VCluster{}, errMaxCapacity
}

func (p *Pool) Flush() {
	for range flushTick.C {
		p.mux.Lock()

		bytes, err := json.Marshal(p.vclusters)
		if err != nil {
			p.l.Error("Fail to marshal", zap.Error(err))
			break
		}

		p.mux.Unlock()

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "vktty-data",
			},
			Data: map[string][]byte{
				"vclusters": bytes,
			},
			Type: corev1.SecretTypeOpaque,
		}

		// Create the Secret
		_, err = p.k.CoreV1().Secrets("default").Update(context.TODO(), secret, metav1.UpdateOptions{})
		if err != nil {
			_, err = p.k.CoreV1().Secrets("default").Create(context.TODO(), secret, metav1.CreateOptions{})
			if err != nil {
				p.l.Error("Flush", zap.Error(err))
			}
		}

		p.l.Info("Flush")
	}
}

func (p *Pool) Start() {
	for range recycleTick.C {
		p.mux.Lock()
		for i, v := range p.vclusters {
			if p.isEOL(v) {
				v.Status = Deleting
				v.Created = nil
				p.vclusters[i] = v
				go p.Delete(i)
			}
		}
		p.mux.Unlock()
	}
}

func (p *Pool) isEOL(v *VCluster) bool {
	return v != nil &&
		(v.Created != nil && time.Now().After(v.Created.Add(p.config.Lifetime)) ||
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
	cmd.Stderr = stderr

	err := cmd.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			p.Error(li, i, "Unexpected", zap.Error(err), zap.String("stdout", stdout.String()), zap.String("stderr", stderr.String()))
			return
		}
	}

	var res execResult
	err = json.Unmarshal(stdout.Bytes(), &res)
	if err != nil {
		p.Error(li, i, "Parsing", zap.Error(err), zap.String("stdout", stdout.String()), zap.String("stderr", stderr.String()))
		return
	}
	if res.Status != 0 {
		errAlreadyExists := "already exists"
		if strings.Contains(stderr.String(), errAlreadyExists) {
			p.Error(li, i, "Error", zap.Int("status_code", res.Status), zap.String("reason", "already exists"))
			return
		}
		errNotFound := "couldn't find vcluster"
		if strings.Contains(stderr.String(), errNotFound) {
			p.Set(li, i, nil, "Reset", zap.Int("status_code", res.Status), zap.String("reason", "Not found"))
			return
		}
		p.Error(li, i, "Unknown Error", zap.Int("status_code", res.Status), zap.String("stdout", stdout.String()), zap.String("stderr", stderr.String()))
		return
	}

	p.mux.Lock()
	p.vclusters[i] = callback(p.vclusters[i], res)
	p.mux.Unlock()

	li.Info("Succeeded")
}

func (p *Pool) Error(l *zap.Logger, i int, msg string, f ...zapcore.Field) {
	p.mux.Lock()
	p.vclusters[i].Status = Error
	p.mux.Unlock()
	l.Error(msg, f...)
}

func (p *Pool) Set(l *zap.Logger, i int, v *VCluster, msg string, f ...zapcore.Field) {
	p.mux.Lock()
	p.vclusters[i] = v
	p.mux.Unlock()
	l.Info(msg, f...)
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
