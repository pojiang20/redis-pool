package main

import (
	"fmt"
	redigo "github.com/gomodule/redigo/redis"
	"time"
)

func main() {
	var addr = "127.0.0.1:6379"
	var password = ""

	pool := PoolInitRedis(addr, password)
	connGroup := make([]redigo.Conn, 0, 5)
	for i := 0; i < 5; i++ {
		connGroup = append(connGroup, pool.Get())
	}
	fmt.Printf("connGroup have %d conn\n", len(connGroup))
	fmt.Printf("idle conn: %d,active conn: %d\n", pool.IdleCount(), pool.ActiveCount())

	for _, v := range connGroup {
		v.Close()
	}
	fmt.Println("close 5 conn")
	fmt.Printf("idle conn: %d,active conn: %d\n", pool.IdleCount(), pool.ActiveCount())

	//下次是怎么取出来的？？
	b1 := pool.Get()
	b2 := pool.Get()
	b3 := pool.Get()
	fmt.Printf("idle conn: %d,active conn: %d\n", pool.IdleCount(), pool.ActiveCount())
	b1.Close()
	b2.Close()
	b3.Close()
	fmt.Println("close 3 conn")
	fmt.Printf("idle conn: %d,active conn: %d\n", pool.IdleCount(), pool.ActiveCount())
}

// redis pool
func PoolInitRedis(server string, password string) *redigo.Pool {
	return &redigo.Pool{
		MaxIdle:     2, //空闲数
		IdleTimeout: 240 * time.Second,
		MaxActive:   3, //最大数
		Dial: func() (redigo.Conn, error) {
			c, err := redigo.Dial("tcp", server)
			if err != nil {
				return nil, err
			}
			if password != "" {
				if _, err := c.Do("AUTH", password); err != nil {
					c.Close()
					return nil, err
				}
			}
			return c, err
		},
		TestOnBorrow: func(c redigo.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
}
