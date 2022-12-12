package main

import (
	"fmt"
	goredis "github.com/go-redis/redis"
	"testing"
)

func Test_goredis(t *testing.T) {
	var addr = "127.0.0.1:6379"
	var password = ""

	c := goredis.NewClient(&goredis.Options{
		Addr:     addr,
		Password: password,
	})
	p, err := c.Ping().Result()
	if err != nil {
		fmt.Println("redis kill")
	}
	fmt.Println(p)
	c.Do("SET", "hello", "world")
	rs := c.Do("GET", "hello").Val()
	fmt.Println(rs)
	c.Close()
}
