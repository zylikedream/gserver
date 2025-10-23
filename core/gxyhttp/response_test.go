package gxyhttp

import (
	"testing"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gstructs"
	"github.com/gogf/gf/v2/util/gconv"
)

type TestData struct {
	g.Meta `path:"/test" method:"POST"`
	ID     int64  `json:"id"`
	Msg    string `json:"msg"`
}

func TestResponse(t *testing.T) {
	resp := &Response{
		Code:    0,
		Message: "success",
		Data:    TestData{ID: 1, Msg: "hello"},
	}
	c, _ := gjson.Marshal(resp)
	t.Log(string(c))
	resp1 := &Response{}
	gjson.Unmarshal(c, &resp1)
	t.Log(resp1)
	t1 := &TestData{}
	gconv.Struct(resp1.Data, t1)
	f, _ := gstructs.TagFields(t1, []string{"path"})
	t.Log(f[0].TagValue)
}
