package pgwire

// sslNegotiationResponse 是对 SSLRequest 的单字节 'N' 响应，表示不支持 SSL。
type sslNegotiationResponse struct{}

// Backend 满足 pgproto3.BackendMessage 接口（无字段标签）。
func (sslNegotiationResponse) Backend() {}

// Encode 将 'N' 字节追加到 dst。
func (sslNegotiationResponse) Encode(dst []byte) ([]byte, error) {
	return append(dst, 'N'), nil
}

// Decode 是 BackendMessage 接口的空实现（此消息仅由服务端发送，无需解码）。
func (sslNegotiationResponse) Decode([]byte) error { return nil }
