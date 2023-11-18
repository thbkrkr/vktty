package main

import (
	"bytes"
	"encoding/base64"
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
)

const (
	lifetime = 3 * time.Minute

	vkttyPath = "./deploy/vktty.sh"
)

var (
	domain = "vktty.miaou.space"

	poolCapacity         = 1
	poolSize             = 5
	poolParallelCreation = 3

	recycleTick = time.NewTicker(10 * time.Second)
)

func main() {
	pool := NewPool(poolCapacity, poolSize)

	r := gin.Default()

	r.GET("/ls", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"vclusters": pool.Ls(),
		})
	})

	r.GET("/sudo/ls", func(c *gin.Context) {
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
			"🐰": fmt.Sprintf("http://z:%s@%s:3132%d", vcluster.password, domain, vcluster.ID),
		})
	})

	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	r.Run()
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
	Password *string    `json:",omitempty"`
	password string
	Status   Status
}

type Pool struct {
	vclusters []*VCluster
	mux       sync.Mutex
}

func NewPool(capacity int, size int) *Pool {
	p := &Pool{
		vclusters: make([]*VCluster, size),
		mux:       sync.Mutex{},
	}

	for i := 0; i < capacity; i++ {
		go func(i int) {
			p.mux.Lock()
			p.vclusters[i] = &VCluster{ID: i, Status: Creating}
			p.mux.Unlock()

			err := p.Add(i)
			if err != nil {
				log.Println("While creating: ", err)
			}
		}(i)
	}

	go p.Recycle()

	return p
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
	for i := range vclusters {
		password := vclusters[i].password
		if password != "" {
			vclusters[i].Password = &password
		}
	}
	return vclusters
}

func (p *Pool) Get() (VCluster, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	for i, v := range p.vclusters {
		if v != nil && v.Status == Free {

			log.Println("Lock c", v.ID)
			now := time.Now()
			p.vclusters[i].Creation = &now
			p.vclusters[i].Status = Locked

			return *v, nil
		}
	}
	creating := 0
	for i, v := range p.vclusters {
		if v != nil && v.Status == Creating {
			creating++
		}
		if v == nil && creating < poolParallelCreation {
			p.vclusters[i] = &VCluster{ID: i, Status: Creating}

			go func(i int) {
				err := p.Add(i)
				if err != nil {
					log.Println("While locking:", err)
				}
			}(i)
			return VCluster{}, errors.New("please come back in 1 min")
		}
	}
	return VCluster{}, errors.New("service too busy")
}

func (p *Pool) Recycle() {
	for range recycleTick.C {
		p.mux.Lock()
		for i, v := range p.vclusters {
			if isEOL(v) {
				v.Status = "deleting"
				v.Creation = nil
				p.vclusters[i] = v

				go func(i int) {
					err := p.Do(i, "delete", func(*VCluster, cmdResult) *VCluster {
						return nil
					})
					if err != nil {
						log.Println("While recycling:", err)
					}
				}(i)
			}
		}
		p.mux.Unlock()
	}
}

func isEOL(v *VCluster) bool {
	return v != nil &&
		(v.Creation != nil && time.Now().After(v.Creation.Add(lifetime)) ||
			v.Status == Error)

}

type cmdResult struct {
	Status   int
	Password string
	Log      string
}

func (p *Pool) Add(i int) error {
	return p.Do(i, "create", func(v *VCluster, r cmdResult) *VCluster {
		v.password = r.Password
		v.Status = Free
		return v
	})
}

func (p *Pool) errorf(format string, action string, i int, err error, a ...any) error {
	p.mux.Lock()
	p.vclusters[i] = &VCluster{ID: i, Status: Error}
	p.mux.Unlock()
	return fmt.Errorf(format, action, i, err, a)
}

func (p *Pool) Do(i int, action string, callback func(*VCluster, cmdResult) *VCluster) error {
	log.Printf("Start %s c%d", action, i)

	cmd := exec.Command(vkttyPath, action, fmt.Sprintf("%d", i))
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdout)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	err := cmd.Run()
	if err != nil {
		log.Printf("error: fail to do %s c%d, err: %s, out: %s", action, i, err, stderr.String())
	}

	var ret cmdResult
	err = json.Unmarshal(stdout.Bytes(), &ret)
	if err != nil {
		return p.errorf("fail to parse %s result for c%d, err: %s, out: %s", action, i, err, stdout.String())
	}
	if ret.Status != 0 {
		log, _ := base64.StdEncoding.DecodeString(ret.Log)
		return p.errorf("fail to %s c%d, err: %s, log: %s", action, i, fmt.Errorf("status code %d", ret.Status), stdout.String(), log)
	}

	p.mux.Lock()
	p.vclusters[i] = callback(p.vclusters[i], ret)
	p.mux.Unlock()

	log.Printf("End %s c%d", action, i)

	return nil
}

func (p *Pool) Delete(action string, i int) error {
	return p.Do(i, "delete", func(*VCluster, cmdResult) *VCluster {
		return nil
	})
}
