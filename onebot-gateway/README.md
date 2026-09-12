### 这个包是对获得数据的统一化
流程: main.go启动app.go当中的ws-server服务器，服务器启动协程读取客户端（napcat/bilbili live）的内容
从json当中获取post_type和字type分发事件
我们从handlers当中拿到处理行为集合(只有最后一个注册的行为返回的action有效)，直接在集合当中添加函数，这个函数会自行执行
### 函数定义的方式

```go
func RegisterMessagePrivateEchoAction() {
	MessagePrivateActions.Add(func(event MessagePrivateEvent) action.Action {
		return action.SendPrivateMsg(action.SendPrivateMsgParams{
			UserID:  event.UserID,
			Message: event.RawMessage,
		})
	})
}
// 现在的上传都调用这个传个空action跑路了
func uploadMemory(e event.MessageGroupEvent) action.Action {
	memory.Upload(context.Background(), e)
	return action.Action{}
}
```

### 后续应该只需要不停的往这里面堆函数处理就行，但是现在action拿不到
