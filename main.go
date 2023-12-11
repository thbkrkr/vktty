package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdlog "log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	env "github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	port = 8042
)

var (
	recycleTick = time.NewTicker(10 * time.Second)

	errCreating                = errors.New("please come back in a moment")
	errMaxPoolParallelCreation = errors.New("please come back later")
	errMaxCapacity             = errors.New("max capacity")

	execTimeout = 3 * time.Minute
)

type Config struct {
	Lifetime             time.Duration
	PoolCapacity         int `envconfig:"POOL_CAPACITY"`
	PoolSize             int `envconfig:"POOL_SIZE"`
	PoolParallelCreation int `envconfig:"POOL_PARALLEL_CREATION"`

	Domain string
	Blurb  string

	ScriptPath string `envconfig:"SCRIPT_PATH"`
}

type App struct {
	cfg  Config
	pool *Pool
	api  *gin.Engine
}

func main() {
	var cfg Config
	err := env.Process("vktty", &cfg)
	if err != nil {
		stdlog.Fatal("fail to process env:", err.Error())
	}

	logger := newLogger(cfg)
	logger.Info("Start",
		zap.String("env", Env(cfg)),
		zap.String("config", fmt.Sprintf("+%v", cfg)))
	defer logger.Sync()

	newMetrics()

	if !isDev(cfg) {
		gin.SetMode(gin.ReleaseMode)
	}

	App{
		cfg:  cfg,
		pool: NewPool(cfg, logger),
		api:  gin.Default(),
	}.Run()
}

func (app App) Run() {

	auth := gin.BasicAuth(gin.Accounts{
		"admin": app.cfg.Blurb,
	})

	app.api.GET("/ls", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"vclusters": app.pool.Ls(),
		})
	})

	app.api.GET("/sudo/ls", auth, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"vclusters": app.pool.SudoLs(),
		})
	})

	app.api.GET("/get", func(c *gin.Context) {
		vcluster, err := app.pool.GetOrCreate()
		if err != nil {
			c.JSON(statusCode(err), gin.H{
				"msg": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"msg": fmt.Sprintf("http://z:%s@%s:3132%d", vcluster.key, app.cfg.Domain, vcluster.ID),
		})
	})

	app.api.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"msg": "🐰",
		})
	})

	app.api.GET("/ready", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"msg": app.pool.ready,
		})
	})

	app.api.GET("/info", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"parallel_creation": app.cfg.PoolParallelCreation,
			"capacity":          app.cfg.PoolCapacity,
			"size":              app.cfg.PoolSize,
			"lifetime":          app.cfg.Lifetime.String(),
		})
	})

	app.api.GET("/metrics", gin.WrapH(promhttp.Handler()))

	app.api.Run(fmt.Sprintf(":%d", port))
}

var statusCodes = map[error]int{
	errCreating:                http.StatusAccepted,
	errMaxPoolParallelCreation: 509, // Bandwidth Limit Exceeded
	errMaxCapacity:             http.StatusServiceUnavailable,
}

func statusCode(err error) int {
	statusCode, ok := statusCodes[err]
	if !ok {
		return http.StatusInternalServerError
	}
	return statusCode
}

var (
	createCounter = prometheus.NewCounter(prometheus.CounterOpts{Name: "vk_create_count"})
	deleteCounter = prometheus.NewCounter(prometheus.CounterOpts{Name: "vk_delete_count"})
	getCounter    = prometheus.NewCounter(prometheus.CounterOpts{Name: "vk_get_count"})
	cmdCounters   = map[vclusterCmd]prometheus.Counter{
		createCmd: createCounter,
		deleteCmd: deleteCounter,
		getCmd:    getCounter,
	}
	buckets      = []float64{1, 5, 10, 20, 30, 45, 60, 120}
	taskDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "vk_cmd_duration",
		Buckets: buckets,
	}, []string{"cmd"})
)

func newMetrics() {
	prometheus.MustRegister(createCounter)
	prometheus.MustRegister(deleteCounter)
	prometheus.MustRegister(taskDuration)
}

type Status string

var (
	Creating Status = "Creating"
	Free     Status = "Free"
	Locked   Status = "Locked"
	Deleting Status = "Deleting"
	Error    Status = "Error"
	EOL      Status = "EOL"

	Pending         Status = "Pending"
	PodInitializing Status = "PodInitializing"
	Init            Status = "Init:0/1"
	Running         Status = "Running"
)

type VCluster struct {
	Name    string
	ID      int
	Created *time.Time `json:",omitempty"`
	Key     string     `json:",omitempty"`
	key     string
	Status  Status
}

func NewVCluster(i int) *VCluster {
	return &VCluster{Name: fmt.Sprintf("c%d", i), ID: i, Status: Creating}
}

func (v *VCluster) isEOL(lifetime time.Duration) bool {
	return v != nil && v.Created != nil && time.Now().After(v.Created.Add(lifetime))
}

func (v *VCluster) isDeleteable(lifetime time.Duration) bool {
	if v == nil || v.Status == Deleting {
		return false
	}
	return v.isEOL(lifetime) || (v.Status == Error || v.Status == EOL)
}

type Pool struct {
	log         *zap.Logger
	config      Config
	vclusters   []*VCluster
	mux         *sync.RWMutex
	vclusterCli vclusterCli
	ready       bool
}

const syncRetry = 1

func NewPool(cfg Config, log *zap.Logger) *Pool {
	p := &Pool{
		log:         log,
		config:      cfg,
		vclusters:   make([]*VCluster, cfg.PoolSize),
		mux:         &sync.RWMutex{},
		vclusterCli: vclusterCli{scriptPath: cfg.ScriptPath},
		ready:       false,
	}
	p.Sync(syncRetry)
	p.Precreate()
	p.ready = true
	go p.Garbage()
	return p
}

func (p *Pool) Precreate() {
	goal := p.config.PoolCapacity - (p.Count(Free) + p.Count(Creating))
	p.log.Info("Precreate", zap.Int("goal", goal), zap.Int("capacity", p.config.PoolCapacity))
	if goal <= 0 {
		return
	}

	p.mux.Lock()
	defer p.mux.Unlock()

	for i := 0; i < len(p.vclusters)-1 && goal > 0; i++ {
		if p.vclusters[i] == nil {
			p.vclusters[i] = NewVCluster(i)
			go p.Add(i)
			goal--
		}
	}
}

func (p *Pool) Count(s Status) int {
	c := 0

	p.mux.RLock()
	defer p.mux.RUnlock()

	for _, v := range p.vclusters {
		if v != nil && v.Status == s {
			c++
		}
	}
	return c
}

// Sync calls vcluster list and adopt existing vclusters if running and not EOL,
// otherwise delete them.
func (p *Pool) Sync(retry int) {
	if retry <= 0 {
		return
	}
	log := p.log.With(zap.Int("retry", retry))
	log.Info("Sync start")

	vclusters, err := p.vclusterCli.list()
	if err != nil {
		log.Error("Fail to sync: parsing cmd error", zap.Error(err))
		return
	}

	for _, v := range vclusters {
		v := v

		// extract ID from name c$i
		i, err := strconv.Atoi(strings.Replace(v.Name, "c", "", -1))
		if err != nil {
			// should not happen
			log.Error("Fail to sync parsing name", zap.Error(err))
			return
		}
		v.ID = i
		prevStatus := v.Status
		resync := false

		switch v.Status {
		case Running:
			if !v.isEOL(p.config.Lifetime) && !v.isDeleteable(p.config.Lifetime) {
				// running and !EOL, let's keep it alive until it's EOL
				v.Status = Free //or Locked? FIXME
				res, err := p.vclusterCli.Exec(p.log, getCmd, i)
				if err == nil {
					if res.Key != "" {
						v.key = res.Key
					} else {
						log.Error("Fail to sync reading key", zap.Error(errors.New("empty key")))
						v.Status = Error
					}
				} else {
					v.Status = Error
				}
			} else {
				// requests its deletion
				v.Status = EOL
			}
		case Pending, Init, PodInitializing:
			resync = true
		default:
			v.Status = Error
		}

		if resync {
			time.AfterFunc(30*time.Second, func() {
				retry--
				go p.Sync(retry)
			})
		}

		p.vclusters[i] = &v
		log.Info("Sync", zap.Int("id", i), zap.String("k", string(v.key)), zap.String("status", string(v.Status)), zap.String("prevStatus", string(prevStatus)))
	}
}

func (p *Pool) Garbage() {
	for range recycleTick.C {
		p.mux.Lock()
		for i, v := range p.vclusters {
			if v.isDeleteable(p.config.Lifetime) {
				p.vclusters[i].Status = Deleting
				go p.Delete(i)
			}
		}
		p.mux.Unlock()
	}
}

func (p *Pool) Ls() []VCluster {
	p.mux.RLock()
	defer p.mux.RUnlock()

	vclusters := []VCluster{}
	for _, v := range p.vclusters {
		if v != nil {
			vclusters = append(vclusters, *v)
		}
	}
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

func (p *Pool) GetOrCreate() (VCluster, error) {
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
			p.log.Info("Lock", zap.Int("id", v.ID))
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
			p.vclusters[i] = NewVCluster(i)
			go p.Add(i)
			return VCluster{}, errCreating
		}
	}

	return VCluster{}, errMaxCapacity
}

func (p *Pool) Add(i int) {
	p.Exec(createCmd, i, func(v *VCluster, r execResult) *VCluster {
		v.key = r.Key
		v.Status = Free
		return v
	})
}

func (p *Pool) Delete(i int) {
	p.Exec(deleteCmd, i, func(*VCluster, execResult) *VCluster {
		// remove the vcluster
		return nil
	})
}

func (p *Pool) Exec(vCmd vclusterCmd, i int, callback func(*VCluster, execResult) *VCluster) {
	res, err := p.vclusterCli.Exec(p.log, vCmd, i)

	p.mux.Lock()
	defer p.mux.Unlock()

	if err == errNotFound {
		p.vclusters[i] = nil
		return
	}
	if err != nil {
		p.vclusters[i].Status = Error
		return
	}

	if callback != nil {
		p.vclusters[i] = callback(p.vclusters[i], res)
	}
}

type execResult struct {
	Status int
	Key    string
}

type vclusterCli struct {
	scriptPath string
}

type vclusterCmd string

const (
	createCmd = vclusterCmd("create")
	deleteCmd = vclusterCmd("delete")
	getCmd    = vclusterCmd("get")
)

var (
	errAlreadyExists = errors.New("already exists")
	errNotFound      = errors.New("couldn't find vcluster")
)

func (c vclusterCli) Exec(l *zap.Logger, vCmd vclusterCmd, i int) (execResult, error) {
	log := l.With(zap.Int("id", i), zap.String("cmd", string(vCmd)))
	log.Info("Exec start")

	start := time.Now()

	duration := func() time.Duration { return time.Since(start) }
	defer func() {
		// collect metrics
		if c, ok := cmdCounters[vCmd]; ok {
			c.Inc()
		}
		taskDuration.WithLabelValues(string(vCmd)).Observe(duration().Seconds())
	}()

	// exec $scriptPath <task> <i>
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.scriptPath, string(vCmd), fmt.Sprintf("%d", i))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err != nil {
		reason := "Unexpected"
		log.Error(reason, zap.Error(err), zap.String("stdout", stdout.String()), zap.String("stderr", stderr.String()))
		return execResult{}, err
	}

	// parse stdout to get the command status and the key
	var res execResult
	err = json.Unmarshal(stdout.Bytes(), &res)
	if err != nil {
		reason := "Parsing"
		log.Error(reason, zap.Error(err), zap.String("stdout", stdout.String()), zap.String("stderr", stderr.String()))
		return execResult{}, err
	}

	// something bad happened, let's look at stderr
	if res.Status != 0 {
		if strings.Contains(stderr.String(), errAlreadyExists.Error()) {
			log.Error("AlreadyExists", zap.Error(err))
			return execResult{}, errAlreadyExists
		}
		if strings.Contains(stderr.String(), errNotFound.Error()) {
			log.Error("NotFound", zap.Error(err))
			return execResult{}, errNotFound
		}
		reason := "Unknown"
		err := fmt.Errorf("status code %d: %s", res.Status, stderr.String())
		log.Error(reason, zap.Error(err), zap.String("stdout", stdout.String()), zap.String("stderr", stderr.String()))
		return execResult{}, err
	}

	log.Info("Exec success", zap.Duration("duration", duration()))
	return res, nil
}

func (c vclusterCli) list() ([]VCluster, error) {
	cmd := exec.Command("vcluster", "ls", "--output=json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	var vclusters []VCluster
	err = json.Unmarshal(out, &vclusters)
	if err != nil {
		return nil, err
	}
	return vclusters, nil
}

func newLogger(c Config) *zap.Logger {
	var z zap.Config
	if !isDev(c) {
		z = zap.NewDevelopmentConfig()
		z.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		z = zap.NewProductionConfig()
		z.Sampling = nil
		z.EncoderConfig.TimeKey = "timestamp"
		z.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}
	return zap.Must(z.Build())
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
