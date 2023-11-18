package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const lifetime = 1 * time.Minute

var timer = time.NewTicker(5 * time.Second)

func main() {
	pool := NewPool(1, 3)

	r := gin.Default()
	r.GET("/lock", func(c *gin.Context) {
		vcluster, err := pool.Get()
		if err != nil {
			log.Println("error: ", err)
			c.JSON(http.StatusOK, gin.H{
				"message": "service too busy",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": fmt.Sprintf("vcluster c%d locked", vcluster.ID),
		})
	})
	r.GET("/ls", func(c *gin.Context) {
		pool.mux.Lock()
		vclusters := pool.vclusters
		pool.mux.Unlock()
		c.JSON(http.StatusOK, gin.H{
			"vclusters": vclusters,
		})
	})
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})
	r.Run()
}

func SendMail(auth string) error {
	return nil
}

type VCluster struct {
	ID       int
	Creation *time.Time
	Status   string
	Auth     string
}

type Pool struct {
	vclusters []*VCluster
	mux       sync.Mutex
}

func NewPool(capacity int, size int) *Pool {
	pool := &Pool{
		vclusters: make([]*VCluster, size),
		mux:       sync.Mutex{},
	}

	for i := 0; i < capacity; i++ {
		go pool.Add(i)
	}

	go pool.Recycle()

	return pool
}

func (p *Pool) Get() (VCluster, error) {
	p.mux.Lock()
	defer p.mux.Unlock()

	for i, v := range p.vclusters {
		// if v != nil && v.Status == "creating" {
		// 	return VCluster{}, errors.New("come back in 1 min")
		// }
		if v != nil && v.Status == "free" {

			log.Println("Lock vcluster ", v.ID)
			now := time.Now()
			v.Creation = &now
			v.Auth = "random"
			v.Status = "used"
			p.vclusters[i] = v

			return *v, nil
		}
		if v == nil {
			go func(i int) {
				err := p.Add(i)
				if err != nil {
					log.Println("While locking: ", err)
				}
			}(i)
			return VCluster{}, errors.New("come back in 1 min")
		}
	}
	return VCluster{}, errors.New("no vcluster available")
}

func (p *Pool) Recycle() {
	for range timer.C {
		p.mux.Lock()
		vclusters := p.vclusters
		p.mux.Unlock()

		for i, v := range vclusters {
			if v != nil && v.Creation != nil && time.Now().After(v.Creation.Add(lifetime)) {

				v.Status = "recycling"
				v.Creation = nil

				log.Println("Recycling vcluster ", v.ID)
				p.mux.Lock()
				p.vclusters[i] = v
				p.mux.Unlock()

				go func(i int) {
					err := p.Delete(i)
					if err != nil {
						log.Println("While recycling: ", err)
					}
				}(i)
			}
		}
	}
}

func (p *Pool) Add(i int) error {
	log.Printf("Creating %d", i)
	p.mux.Lock()
	p.vclusters[i] = &VCluster{ID: i, Status: "creating"}
	p.mux.Unlock()

	out, err := exec.Command("./vktty.sh", "create", fmt.Sprintf("%d", i)).Output()
	if !strings.Contains(string(out), "Successfully created virtual cluster") && err != nil {
		return fmt.Errorf("fail to create vcluster %d: %s, %s", i, err, string(out))
	}
	log.Println(string(out))

	p.mux.Lock()
	p.vclusters[i].Status = "free"
	p.mux.Unlock()

	return nil
}

func (p *Pool) Delete(i int) error {
	log.Printf("Deleting %d", i)
	p.mux.Lock()
	p.vclusters[i] = &VCluster{ID: i, Status: "deleting"}
	p.mux.Unlock()

	out, err := exec.Command("./vktty.sh", "delete", fmt.Sprintf("%d", i)).Output()
	if err != nil {
		return fmt.Errorf("fail to delete vcluster %d: %s, %s", i, err, string(out))
	}
	log.Println(string(out))

	p.mux.Lock()
	p.vclusters[i].Status = "free"
	p.mux.Unlock()

	return nil
}
