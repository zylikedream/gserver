package gxyproto

type ReqHandShake struct {
	AccountUid string `json:"account_uid"`
}

type RspHandShake struct {
	AccountUid string `json:"account_uid"`
}

type ReqAccountLogin struct {
	AccountUid string `json:"account_uid"`
	Client     string `json:"client"`
}

type RspAccountLogin struct {
	FirstLogin bool `json:"first_login"`
}
