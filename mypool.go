package main

import (
	"errors"
	"sync"
	"time"
)

const (
	TIMEOUT = time.Second
)

type myConn struct {
}

type myPool struct {
	config struct {
		MinIdleConns  int
		LimitPoolSize int
	}

	queue chan struct{}
	lock  sync.Mutex

	//闲置连接
	idleConns []*myConn
	//现有连接
	Conns    []*myConn
	poolSize int
}

// 外部使用的get
func (p *myPool) Get() *myConn {
	//池子大小
	p.waitTurn()

	for {
		p.lock.Lock()
		idleConn := p.getIdle()
		p.lock.Unlock()

		if idleConn == nil {
			break
		}
		//查看是否有效
		if p.isStaleConn(idleConn) {
			p.CloseConn(idleConn)
			continue
		}
		return idleConn
	}
	//新建连接
	return p.NewConn()
}

// 外部使用的put
func (p *myPool) Put(conn *myConn) {
	p.lock.Lock()
	p.idleConns = append(p.idleConns, conn)
	p.lock.Unlock()
	//数量控制
	p.freeTurn()
}

// 关闭连接
func (p *myPool) CloseConn(conn *myConn) {

}

// 新建连接
func (p *myPool) NewConn() *myConn {
	return nil
}

// 判断连接是否有效
func (p *myPool) isStaleConn(conn *myConn) bool {
	return false
}

// 访问控制
func (p *myPool) waitTurn() error {
	timer := time.NewTicker(TIMEOUT)
	select {
	case <-timer.C:
		return errors.New("wait timeout")
	case p.queue <- struct{}{}:
		return nil
	}
}
func (p *myPool) freeTurn() {
	<-p.queue
}

func (p *myPool) getIdle() (idleConn *myConn) {
	if len(p.idleConns) == 0 {
		return nil
	}

	//从队尾获取数据，并维护最小闲置连接数
	idleConn, p.idleConns = p.idleConns[len(p.idleConns)-1], p.idleConns[:len(p.idleConns)-1]
	p.checkMinIdleConns()
	return
}

// 维护最小连接数
func (p *myPool) checkMinIdleConns() {
	if p.config.MinIdleConns == 0 {
		return
	}
	//维护最小闲置连接数量
	if p.poolSize < p.config.LimitPoolSize && len(p.idleConns) < p.config.LimitPoolSize {
		p.poolSize++
		go func() {
			newConn := p.NewConn()
			p.lock.Lock()
			p.idleConns = append(p.idleConns, newConn)
			p.Conns = append(p.Conns, newConn)
			p.lock.Unlock()
		}()
	}
}

// 独立协程来清理过期连接
func (p *myPool) reaper(frequency time.Duration) {
	ticker := time.NewTicker(frequency)
	defer ticker.Stop()

	for range ticker.C {
		p.ReapStaleConns()
	}
}

// 根据条件清除部分过期连接
func (p *myPool) ReapStaleConns() (int, error) {
	return 0, nil
}
