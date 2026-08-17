package logic

import (
	"context"
	_ "embed"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gserver/core/gxyactor"
	"gserver/core/gxyhttp"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic/bag"
	"gserver/src/lib/rolelib"

	"github.com/cockroachdb/errors"
	"github.com/gogf/gf/v2/util/gconv"
)

type RoleGM struct {
	RoleModule
	ctx context.Context
}

var _ IRoleModule = (*RoleGM)(nil)

// ========== 帮助提取 ==========

type CmdDoc struct {
	Name    string
	Brief   string
	Usage   string
	Example string
}

var (
	gmCmdDocs []CmdDoc
	gmCmdMap  map[string]reflect.Method
	gmInited  bool
)

func gmEnsureInited() {
	if gmInited {
		return
	}
	gmInited = true
	gmCmdDocs = gmExtractDocs()
	gmCmdMap = gmBuildCmdMap()
}

//go:embed role_gm.go
var gmSource string

// gmExtractDocs 解析 role_gm.go 源码，提取带 "用法:" 注释的方法作为 GM 命令
func gmExtractDocs() []CmdDoc {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "role_gm.go", gmSource, parser.ParseComments)
	if err != nil {
		return nil
	}

	p, err := doc.NewFromFiles(fset, []*ast.File{f}, "./")
	if err != nil {
		return nil
	}
	for _, t := range p.Types {
		if t.Name != "RoleGM" {
			continue
		}
		var docs []CmdDoc
		for _, m := range t.Methods {
			if !m.Decl.Name.IsExported() {
				continue
			}
			if !strings.Contains(m.Doc, "用法:") && !strings.Contains(m.Doc, "用法：") {
				continue
			}
			cmdName := camelToSnake(m.Name)
			d := gmParseCmdDoc(cmdName, m.Doc)
			docs = append(docs, d)
		}
		return docs
	}
	return nil
}

func gmParseCmdDoc(name string, docStr string) CmdDoc {
	d := CmdDoc{Name: name}
	lines := strings.Split(docStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if d.Brief == "" {
			d.Brief = line
		}
		if strings.HasPrefix(line, "用法:") || strings.HasPrefix(line, "用法：") {
			d.Usage = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "用法:"), "用法："))
		}
		if strings.HasPrefix(line, "示例:") || strings.HasPrefix(line, "示例：") {
			d.Example = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "示例:"), "示例："))
		}
	}
	return d
}

func gmBuildCmdMap() map[string]reflect.Method {
	m := make(map[string]reflect.Method)
	t := reflect.TypeFor[*RoleGM]()
	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i)
		if !method.IsExported() {
			continue
		}
		cmdName := camelToSnake(method.Name)
		if _, ok := m[cmdName]; !ok {
			m[cmdName] = method
		}
	}
	return m
}

func camelToSnake(s string) string {
	var result []rune
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, unicode.ToLower(r))
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}

// ========== 命令路由 ==========

func (r *RoleGM) execCommand(name string, args []string) error {
	gmEnsureInited()
	method, ok := gmCmdMap[name]
	if !ok {
		return errors.Newf("unknown command: %s", name)
	}
	numParams := method.Type.NumIn() - 1 // minus receiver
	if len(args) < numParams {
		return errors.Newf("command %s needs %d arg(s), got %d", name, numParams, len(args))
	}
	if len(args) > numParams && numParams > 0 {
		// join extra args into the last parameter (e.g. system message content with spaces)
		last := numParams - 1
		args[last] = strings.Join(args[last:], " ")
		args = args[:numParams]
	}
	in := make([]reflect.Value, 1+len(args))
	in[0] = reflect.ValueOf(r)
	for i, arg := range args {
		paramType := method.Type.In(i + 1)
		val := gconv.Convert(arg, paramType.Kind().String())
		in[i+1] = reflect.ValueOf(val).Convert(paramType)
	}
	results := method.Func.Call(in)
	if len(results) > 0 {
		if err, ok := results[0].Interface().(error); ok && err != nil {
			return err
		}
	}
	return nil
}

// ========== Proto Handler ==========

func (r *RoleGM) ReqGMCommand(ctx context.Context, req *pb.ReqGMCommand) (*pb.RspGMCommand, error) {
	parts := strings.Fields(req.Cmd)
	if len(parts) == 0 {
		return &pb.RspGMCommand{Result: "empty command"}, nil
	}
	r.ctx = ctx
	err := r.execCommand(parts[0], parts[1:])
	if err != nil {
		return &pb.RspGMCommand{Result: err.Error()}, nil
	}
	return &pb.RspGMCommand{Result: "ok"}, nil
}

func (r *RoleGM) ReqGMHelp(ctx context.Context, req *pb.ReqGMHelp) (*pb.RspGMHelp, error) {
	gmEnsureInited()
	cmds := make([]*pb.PCmdDoc, 0, len(gmCmdDocs))
	for _, d := range gmCmdDocs {
		cmds = append(cmds, &pb.PCmdDoc{
			Name:    d.Name,
			Brief:   d.Brief,
			Usage:   d.Usage,
			Example: d.Example,
		})
	}
	return &pb.RspGMHelp{Commands: cmds}, nil
}

// ========== GM 命令定义 ==========
// 每个命令方法的注释即为帮助文档，格式：
// 第一行：简要说明
// 用法: 命令名 [参数...]
// 示例: 命令名 参数值...
//
// 带 "用法:" 注释的方法会被 go/doc 识别为 GM 命令

// AddGoods 添加物品或货币
// 用法: add_goods [物品ID] [数量]
// 示例: add_goods 1001 10
func (r *RoleGM) AddGoods(itemID int, num int) error {
	return r.Role.Bag.SaveGoods(r.ctx, nil, []*gamecfg.GardenGoodStack{bag.MakeGoodStack(itemID, int(num))}, "gm")
}

// RemoveGoods 移除物品或货币
// 用法: remove_goods [物品ID] [数量]
// 示例: remove_goods 1001 5
func (r *RoleGM) RemoveGoods(itemID int, num int) error {
	return r.Role.Bag.SaveGoods(r.ctx, []*gamecfg.GardenGoodStack{bag.MakeGoodStack(itemID, int(num))}, nil, "gm")
}

// SetPlayerLevel 设置玩家等级（用于测试）
// 用法: set_player_level [等级]
// 示例: set_player_level 10
func (r *RoleGM) SetPlayerLevel(level int) error {
	if level < 1 {
		return errors.Newf("level must be >= 1")
	}
	cfg := r.Cfg().TbPlayerLevel.Get(int32(level))
	if cfg == nil {
		return errors.Newf("player level config not found: %d", level)
	}
	oldExp := r.Role.Basic.getPlayerExp()
	targetExp := int64(cfg.TotalExp)
	if oldExp >= targetExp {
		return nil
	}
	addExp := targetExp - oldExp
	if err := r.Role.Bag.SaveGoods(r.ctx, nil, []*gamecfg.GardenGoodStack{bag.MakeGoodStack(PLAYER_EXP_ITEM_ID, int(addExp))}, "gm"); err != nil {
		return err
	}
	r.Role.Basic.RefreshLevelByExp(r.ctx, oldExp, targetExp, "gm")
	return nil
}

// AddFlower 解锁花的培育权限
// 用法: add_flower [花ID]
// 示例: add_flower 101
func (r *RoleGM) AddFlower(flowerID int) error {
	r.Role.Flower.AddFlower(context.Background(), int32(flowerID))
	return nil
}

// AddFlowerLevel 设置鲜花等级（用于测试）
// 用法: add_flower_level [花ID] [等级]
// 示例: add_flower_level 101 10
func (r *RoleGM) AddFlowerLevel(flowerID int, level int) error {
	flower, ok := r.Role.Flower.Flowers[int32(flowerID)]
	if !ok {
		return errors.Newf("flower %d not unlocked", flowerID)
	}
	if level < 1 {
		return errors.Newf("level must be >= 1")
	}
	flower.Level = int32(level)
	r.Role.Flower.MarkDirty()
	return nil
}

// AddOrderFlower 设置鲜花为已培育完成状态（用于订单生成测试）
// 用法: add_order_flower [花ID]
func (r *RoleGM) AddOrderFlower(flowerID int) error {
	r.Role.Flower.AddFlower(context.Background(), int32(flowerID))
	if flower, ok := r.Role.Flower.Flowers[int32(flowerID)]; ok {
		flower.State = int32(pb.FlowerState_FLOWER_HARVESTED)
	}
	return nil
}

// AddFlowerBreedGoods 一键添加培育所需材料
// 用法: add_flower_breed_goods [花ID]
// 示例: add_flower_breed_goods 101
func (r *RoleGM) AddFlowerBreedGoods(flowerID int) error {
	cfg := r.Cfg().TbFlower.Get(int32(flowerID))
	if cfg == nil {
		return errors.Newf("flower config not found: %d", flowerID)
	}
	return r.Role.Bag.SaveGoods(r.ctx, nil, cfg.BreedCost, "gm")
}

// FinishBreedGM 立即完成当前培育
// 用法: finish_breed
// 示例: finish_breed
func (r *RoleGM) FinishBreed() error {
	breeding := r.Role.Flower.FindBreeding()
	if breeding == nil {
		return errors.Newf("no flower is breeding")
	}
	breeding.StateTime = time.Now().Add(-time.Second)
	r.Role.Flower.MarkDirty()
	return nil
}

// UnlockPlot 解锁地块
// 用法: unlock_plot [地块ID]
// 示例: unlock_plot 1
func (r *RoleGM) UnlockPlot(plotID int) error {
	r.Role.Plot.UnlockPlot(int32(plotID))
	return nil
}

// SendSystemMsg 发送全服系统消息
// 用法: send_system_msg [消息内容]
// 示例: send_system_msg 服务器即将维护，请及时下线
func (r *RoleGM) SendSystemMsg(content string) error {
	return SendSystemMsg(r.ctx, content)
}

func SendSystemMsg(ctx context.Context, content string) error {
	_, err := gxyhttp.HttpSystem().PostService(ctx, "chat-http",
		fmt.Sprintf("store_system?content=%s", url.QueryEscape(content)))
	return err
}

// StopRole 停止指定角色的会话（从内存驱逐)
// 用法: stop_role [RoleID]
// 示例: stop_role 1001 从内存驱逐
func (r *RoleGM) StopRole(roleID int64) error {
	rolePid := rolelib.GetRolePid(roleID)
	if rolePid == nil {
		return errors.Newf("role %d not found ", roleID)
	}
	_ = gxyactor.LocalSend(r.ctx, rolePid, &pb.ActorStop{
		Reason: "gm stop role",
	})
	return nil
}

// SendMail 发送邮件给指定角色
// 用法: send_mail [RoleID] [标题] [内容]
// 示例: send_mail 1001 测试邮件 这是一封测试邮件
func (r *RoleGM) SendMail(roleID int64, title string, content string) error {
	return SendMail(r.ctx, r.Deps(), roleID, SendMailOpts{
		Title:   title,
		Content: content,
	})
}

// SendMailAll 发送全服邮件
// 用法: send_mail_all [标题] [内容]
// 示例: send_mail_all 系统维护补偿 感谢您的支持
func (r *RoleGM) SendMailAll(title string, content string) error {
	return SendMailToAll(r.ctx, r.Deps(), SendMailOpts{
		Title:   title,
		Content: content,
	})
}

// SendMailGoods 发送带附件的邮件
// 用法: send_mail_goods [RoleID] [标题] [内容] [GoodID:Num,...]
// 示例: send_mail_goods 1001 补偿 维护补偿 101:5,102:3
func (r *RoleGM) SendMailGoods(roleID int64, title string, content string, goodsSpec string) error {
	var goods []bag.Good
	for _, part := range strings.Split(goodsSpec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.Split(part, ":")
		if len(kv) != 2 {
			return errors.Newf("invalid goods spec: %s", part)
		}
		goodID, err := strconv.Atoi(strings.TrimSpace(kv[0]))
		if err != nil {
			return errors.Newf("invalid good id: %s", kv[0])
		}
		num, err := strconv.Atoi(strings.TrimSpace(kv[1]))
		if err != nil {
			return errors.Newf("invalid num: %s", kv[1])
		}
		goods = append(goods, bag.Good{GoodID: goodID, Num: uint64(num)})
	}
	return SendMail(r.ctx, r.Deps(), roleID, SendMailOpts{
		Title:       title,
		Content:     content,
		Attachments: goods,
	})
}
