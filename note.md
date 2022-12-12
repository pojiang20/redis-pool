### 介绍
go-redis中用到了连接池，它主要是在处理命令时，在连接池中获取连接。如下面的`getConn()`就是从池中取出连接来做后续命令执行。
```go
func (c *baseClient) defaultProcess(cmd Cmder) error {
	for attempt := 0; attempt <= c.opt.MaxRetries; attempt++ {

		cn, err := c.getConn()
        //do something
	}

	return cmd.Err()
}
```

### 初始化
主要是建立最小闲置连接，根据设置创建指定数量的闲置连接存储在conns和idleConns中。

```go
func NewConnPool(opt *Options) *ConnPool {
    p := &ConnPool{
        opt: opt,   //Save 配置
        // 创建存储工作连接的缓冲通道
        queue:     make(chan struct{}, opt.PoolSize),
        // 创建存储所有 conn 的切片
        conns:     make([]*Conn, 0, opt.PoolSize),
        // 创建存储所有 conn 的切片
        idleConns: make([]*Conn, 0, opt.PoolSize),
        // 用于通知所有协程连接池已经关闭的通道
        closedCh:  make(chan struct{}),
    }

    // 检查连接池的空闲连接数量是否满足最小空闲连接数量要求
    // 若不满足，则创建足够的空闲连接
    p.checkMinIdleConns()

    if opt.IdleTimeout > 0 && opt.IdleCheckFrequency > 0 {
        // 如果配置了空闲连接超时时间 && 检查空闲连接频率
        // 则单独开启一个清理空闲连接的协程（定期）
        // 无限循环判断连接是否过期，过期的连接清理掉
        go p.reaper(opt.IdleCheckFrequency)
    }

    return p
}

// 根据 MinIdleConns 参数，初始化即创建指定数量的连接
func (p *ConnPool) checkMinIdleConns() {
	if p.opt.MinIdleConns == 0 {
		return
	}
	for p.poolSize < p.opt.PoolSize && p.idleConnsLen < p.opt.MinIdleConns {
		p.poolSize++
		p.idleConnsLen++
		go func() {
            // 异步建立 redis 连接，以满足连接池的最低要求
			err := p.addIdleConn()
			if err != nil {
                p.connsMu.Lock()
                 // 创建 redis 失败时则减去计数，注意先 Lock
				p.poolSize--
				p.idleConnsLen--
				p.connsMu.Unlock()
			}
		}()
	}
}

// 空闲连接的建立，向连接池中加新连接
func (p *ConnPool) addIdleConn() error {
	cn, err := p.dialConn(context.TODO(), true)
	if err != nil {
		return err
	}

    // conns idleConns 是共享资源，需要加锁控制
	p.connsMu.Lock()
	defer p.connsMu.Unlock()

	// It is not allowed to add new connections to the closed connection pool.
	if p.closed() {
		_ = cn.Close()
		return ErrClosed
	}

	p.conns = append(p.conns, cn)       // 增加连接队列
	p.idleConns = append(p.idleConns, cn)   // 增加轮转队列
	return nil
}

//真正的初始化连接方法
func NewConn(netConn net.Conn) *Conn {
	cn := &Conn{
		netConn:   netConn,
        // 连接的出生时间点
		createdAt: time.Now(),
	}
	cn.rd = proto.NewReader(netConn)
	cn.wr = proto.NewWriter(netConn)
    // 连接上次使用的时间点
	cn.SetUsedAt(time.Now())
	return cn
}
```
以及启动检查器，根据配置的间隔时间定期清理过期的空闲连接。这里处理过期连接逻辑如下，只有当poolSize没满，并且可以从reapStaleConn()中获取出过期连接，才会将连接移除。
```go
func (p *ConnPool) ReapStaleConns() (int, error) {
	var n int
	for {
		p.getTurn()

		p.connsMu.Lock()
		cn := p.reapStaleConn()
		p.connsMu.Unlock()

		if cn != nil {
			p.removeConn(cn)
		}

		p.freeTurn()

		if cn != nil {
			p.closeConn(cn)
			n++
		} else {
			break
		}
	}
	return n, nil
}
```

### 关闭连接池
将连接池中现存的所有连接关闭。
```go
func (p *ConnPool) Close() error {
    // 原子性检查连接池是否已经关闭，若没关闭，则将关闭标志置为 1
    if !atomic.CompareAndSwapUint32(&p._closed, 0, 1) {
        return ErrClosed
    }

    // 关闭 closedCh 通道
    // 连接池中的所有协程都可以通过判断该通道是否关闭来确定连接池是否已经关闭
    close(p.closedCh)

    var firstErr error
    //Lock
    p.connsMu.Lock()
    // 连接队列锁上锁，关闭队列中的所有连接，并置空所有维护连接池状态的数据结构
    for _, cn := range p.conns {
        if err := p.closeConn(cn); err != nil && firstErr == nil {
            firstErr = err
        }
    }
    p.conns = nil
    p.poolSize = 0
    p.idleConns = nil
    p.idleConnsLen = 0
    p.connsMu.Unlock()

    return firstErr
}
```

## 连接池中单个连接管理
### 建立连接
建立连接并且添加到维护的conns中，会在连接出错次数与pool大小相同时，调用tryDial。tryDial作为一个独立的协程会不断的连接、休息，直到成功建立一个连接为止，这是一个与服务器端建立连接的常见策略。
```go
func (p *ConnPool) tryDial() {
	for {
		if p.closed() {
			return
		}

		conn, err := p.opt.Dialer()
		if err != nil {
			p.setLastDialError(err)
			time.Sleep(time.Second)
			continue
		}

		atomic.StoreUint32(&p.dialErrorsNum, 0)
		_ = conn.Close()
		return
	}
}
```
### 获取连接
获取连接先尝试获取闲置连接，并判断是否过期。存在闲置连接且未过期则可以正常获取，否则创建新的连接。
```go
// Get returns existed connection from the pool or creates a new one.
func (p *ConnPool) Get() (*Conn, error) {
	err := p.waitTurn()
	for {
		p.connsMu.Lock()
		cn := p.popIdle()
		p.connsMu.Unlock()

		if cn == nil {
			break
		}

		if p.isStaleConn(cn) {
			_ = p.CloseConn(cn)
			continue
		}
		return cn, nil
	}

	newcn, err := p._NewConn(true)
	return newcn, nil
}
```
这里waitTurn来判断是否有权限从连接池中获取连接，这里维护固定大小的queue，并且做标记（写入空）。
如果存在缓存则直接返回，否则设置一个定时器来再次写入，在定时器结束之前非阻塞则返回，否则返回阻塞超时错误。
```go
func (p *ConnPool) waitTurn() error {
	select {
	case p.queue <- struct{}{}:
		return nil
	default:
		timer := timers.Get().(*time.Timer)
		timer.Reset(p.opt.PoolTimeout)

		select {
		case p.queue <- struct{}{}:
			if !timer.Stop() {
				<-timer.C
			}
			timers.Put(timer)
			return nil
		case <-timer.C:
			timers.Put(timer)
			atomic.AddUint32(&p.stats.Timeouts, 1)
			return ErrPoolTimeout
		}
	}
}
```
这里提一下Idle这个数据结构，这是一个双端队列，在队列尾部添加和弹出。
```go
func (p *ConnPool) popIdle() *Conn {
	if len(p.idleConns) == 0 {
		return nil
	}

	idx := len(p.idleConns) - 1
	cn := p.idleConns[idx]
	p.idleConns = p.idleConns[:idx]
	p.idleConnsLen--
	p.checkMinIdleConns()
	return cn
}
func (p *ConnPool) Put(cn *Conn) {
    if !cn.pooled {
    p.Remove(cn, nil)
    return
    }
    
    p.connsMu.Lock()
    p.idleConns = append(p.idleConns, cn)
    p.idleConnsLen++
    p.connsMu.Unlock()
    p.freeTurn()
}
```
但处理超时是从队头开始处理。
```go
func (p *ConnPool) reapStaleConn() *Conn {
	if len(p.idleConns) == 0 {
		return nil
	}

	cn := p.idleConns[0]
	if !p.isStaleConn(cn) {
		return nil
	}

	p.idleConns = append(p.idleConns[:0], p.idleConns[1:]...)
	p.idleConnsLen--

	return cn
}
```
这样做是非常合理的，因为队尾一定是频繁put和get的连接，而队头一定是相对变动少的"久远"的连接，队头连接失效的可能性更大。
### 放回连接
将链接放回到闲置队列队尾
```go
func (p *ConnPool) Put(cn *Conn) {
    if !cn.pooled {
    p.Remove(cn, nil)
    return
    }
    
    p.connsMu.Lock()
    p.idleConns = append(p.idleConns, cn)
    p.idleConnsLen++
    p.connsMu.Unlock()
    p.freeTurn()
}
```
### 移除连接
就是逐个匹配连接，然后删除。
```go
func (p *ConnPool) removeConn(cn *Conn) {
    // 遍历连接队列找到要关闭的连接，并将其移除出连接队列
    for i, c := range p.conns {
        if c == cn {
            // 比较指针的值
            p.conns = append(p.conns[:i], p.conns[i+1:]...)
            if cn.pooled {
                // 如果 cn 这个连接是在 pool 中的，更新连接池统计数据
                p.poolSize--
                // 检查连接池最小空闲连接数量，如果不满足最小值，需要异步补充
                p.checkMinIdleConns()
            }
            return
        }
    }
}
```