# waykichain-adapter

本项目适配了openwallet.AssetsAdapter接口，给应用提供了底层的区块链协议支持。

## 如何测试

openwtester包下的测试用例已经集成了openwallet钱包体系，创建conf文件，新建WICC.ini文件，编辑如下内容：

```ini

# node api url
nodeAPI = "http://ip:port"

# rpc user
rpcUser = "user"
# rpc password 
rpcPassword = "password"

# fixed Fee in sawi
fixedFee = 10000
# fixed wrc20 fee in sawi
fixedWRC20Fee = 100000
# register fee in sawi
registerFee = 10000
# min transfer amount in sawi
minTransferAmount = 10000
# Cache data file directory, default = "", current directory: ./data
dataDir = ""
```

# 地址激活
```
以地址"WZa9hSDWcBubNTXs4ukQwobXdGrgo9SWq9"为例:
1. 向待激活地址中转入不少于10000swai(aka 0.0001 WICC)用于激活，注册最低金额如果发生变化，可在配置文件中修改registerFee参数进行同步更改；
2. 创建交易，交易目标地址留空，填入待激活地址所在的walletID和AccountID；
3. 将memo设置为 "register:WZa9hSDWcBubNTXs4ukQwobXdGrgo9SWq9"；
4. 剩下流程与正常交易过程一样。
```
