package thanks

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxylog"
)

type thanksApp struct {
	gxyapp.App
}

func NewThanksApp() *thanksApp {
	return &thanksApp{}
}

func (s *thanksApp) OnModStartAfter(ctx context.Context) error {
	gxylog.Info(ctx, buddha)
	return nil
}

const buddha = "\n" +
	"                _ooOoo_\n" +
	"               o8888888o\n" +
	"               88\" . \"88\n" +
	"               (| -_- |)\n" +
	"               O\\  =  /O\n" +
	"            ____/`---'\\____\n" +
	"           .'  \\\\|     |//  `.\n" +
	"          /  \\\\|||  :  |||//  \\\n" +
	"         /  _||||| -:- |||||_  \\\n" +
	"         |   | \\\\  -  /// |   |\n" +
	"         | \\_|  ''\\---/''  |_/ |\n" +
	"         \\  .-\\__  `-`___/-. /\n" +
	"       ___. .'  /--.--\\  `. . __\n" +
	"    .\"\"<  `.___\\_<|>_/___.'  >'\"\".\n" +
	"   | | :  `- \\`.;`\\ _ /`;.`/ - ` : | |\n" +
	"   \\  \\ `-.   \\_ __\\ /__ _/   .-`/  /\n" +
	"====`-.____`-.___\\_____/___.-`____.-'======\n" +
	"                `=---='"
