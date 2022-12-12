# redis-pool
学习redis池化

main.go中执行的结果如下，pool的初始化配置是maxIdle=2,maxActive=3。
最开始从pool中获取5个连接，由于设置了maxActive=3因此，因此`idle conn: 0,active conn: 3`
再将获取所有的连接关闭，由于设置了maxIdle=2，`idle conn: 2,active conn: 2`，此时一定有idle conn==active conn。
再从pool中获取3个连接，此时`idle conn`用完，`active conn`变为3.
再次将所有连接关闭，恢复到`idle conn: 2,active conn: 2`。
```text
connGroup have 5 conn
idle conn: 0,active conn: 3
close 5 conn
idle conn: 2,active conn: 2
idle conn: 0,active conn: 3
close 3 conn
idle conn: 2,active conn: 2

```