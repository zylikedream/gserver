package gxymongo

import (
	"context"
	"time"

	"gserver/core/gxymodule"

	"gserver/util"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/gogf/gf/v2/os/glog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type mongoClient struct {
	gxymodule.ModuleBase
	client *mongo.Client
	conf   *mongoConfig
}

type mongoConfig struct {
	Addr     string `toml:"addr"`
	DataBase string `toml:"database"`
	PoolSize struct {
		Max int `toml:"max"`
		Min int `toml:"min"`
	} `toml:"pool_size"`
	ConnectTimeout time.Duration `toml:"connect_timeout"`
}

var mgoCli *mongoClient

func Client() *mongoClient {
	return mgoCli
}

func NewMongoClient(config string) *mongoClient {
	cfg := gcfg.Instance(config)
	conf := &mongoConfig{}
	ctx := gctx.New()
	if err := util.CfgUnmarshalKey(ctx, cfg, "mongo", conf); err != nil {
		glog.Fatal(ctx, err)
	}
	mgoCli = &mongoClient{
		conf: conf,
	}
	return mgoCli
}

func (m *mongoClient) OnModInit(ctx context.Context) error {
	opt := options.Client()
	conf := m.conf
	opt.ApplyURI(conf.Addr)
	opt.SetMinPoolSize(uint64(conf.PoolSize.Min))
	opt.SetMaxPoolSize(uint64(conf.PoolSize.Max))
	connTimeout := conf.ConnectTimeout
	opt.SetConnectTimeout(connTimeout)
	opt.SetServerSelectionTimeout(connTimeout)
	opt.SetLoggerOptions(&options.LoggerOptions{
		Sink: NewMongoLogger(),
	})
	client, err := mongo.Connect(ctx, opt)
	if err != nil {
		return err
	}
	m.client = client
	return nil
}
func (m *mongoClient) OnModStart(ctx context.Context) error {
	// 创建一个带超时的context
	// 这里可以使用配置中的连接超时时间，或自定义一个超时时间
	pingTimeout := m.conf.ConnectTimeout
	if pingTimeout <= 0 {
		pingTimeout = 3 * time.Second // 默认5秒超时
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel() // 确保在函数结束时取消context

	if err := m.client.Ping(pingCtx, readpref.Primary()); err != nil {
		return gerror.Wrapf(err, "mongo client ping error: %s", m.conf.Addr)
	}
	glog.Infof(ctx, "[module]mongo start success: %s", m.conf.Addr)
	return nil
}

func (m *mongoClient) OnModStop(ctx context.Context) error {
	if m.client != nil {
		if err := m.client.Disconnect(ctx); err != nil {
			return err
		}
	}
	glog.Info(ctx, "[module]mongo stop success")
	return nil
}

func (m *mongoClient) GetDatabase(ctx context.Context) string {
	return m.conf.DataBase
}

func (m *mongoClient) FindOne(ctx context.Context, reply interface{}, Col string, filter interface{}, opts ...*options.FindOneOptions) error {
	col := m.client.Database(m.GetDatabase(ctx)).Collection(Col)
	return col.FindOne(ctx, filter, opts...).Decode(reply)
}

func (m *mongoClient) Find(ctx context.Context, replys interface{}, Col string, filter interface{}, opts ...*options.FindOptions) error {
	col := m.client.Database(m.GetDatabase(ctx)).Collection(Col)
	csr, err := col.Find(ctx, filter, opts...)
	if err != nil {
		return err
	}
	return csr.All(ctx, replys)
}

func (m *mongoClient) UpdateSetOne(ctx context.Context, Col string, filter interface{}, Set interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	col := m.client.Database(m.GetDatabase(ctx)).Collection(Col)
	return col.UpdateOne(ctx, filter, bson.M{"$set": Set}, opts...)
}

func (m *mongoClient) UpdateOne(ctx context.Context, Col string, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	col := m.client.Database(m.GetDatabase(ctx)).Collection(Col)
	return col.UpdateOne(ctx, filter, update, opts...)
}

func (m *mongoClient) UpdateMany(ctx context.Context, Col string, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	col := m.client.Database(m.GetDatabase(ctx)).Collection(Col)
	return col.UpdateMany(ctx, filter, update, opts...)
}

func (m *mongoClient) ReplaceOne(ctx context.Context, Col string, filter interface{}, update interface{}, opts ...*options.ReplaceOptions) (*mongo.UpdateResult, error) {
	col := m.client.Database(m.GetDatabase(ctx)).Collection(Col)
	return col.ReplaceOne(ctx, filter, update, opts...)
}

func (m *mongoClient) InsertOne(ctx context.Context, Col string, doc interface{}, opts ...*options.InsertOneOptions) (*mongo.InsertOneResult, error) {
	col := m.client.Database(m.GetDatabase(ctx)).Collection(Col)
	return col.InsertOne(ctx, doc, opts...)
}

func (m *mongoClient) InsertMany(ctx context.Context, Col string, docs []interface{}, opts ...*options.InsertManyOptions) (*mongo.InsertManyResult, error) {
	col := m.client.Database(m.GetDatabase(ctx)).Collection(Col)
	return col.InsertMany(ctx, docs, opts...)
}

func (m *mongoClient) DeleteOne(ctx context.Context, Col string, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	col := m.client.Database(m.GetDatabase(ctx)).Collection(Col)
	return col.DeleteOne(ctx, filter, opts...)
}

func (m *mongoClient) DeleteMany(ctx context.Context, Col string, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	col := m.client.Database(m.GetDatabase(ctx)).Collection(Col)
	return col.DeleteOne(ctx, filter, opts...)
}

// WithTransactionWithOptions 执行带选项的MongoDB事务
func (m *mongoClient) WithTransaction(ctx context.Context, fn func(ctx mongo.SessionContext) (any, error), opts ...*options.TransactionOptions) (any, error) {
	session, err := m.client.StartSession()
	if err != nil {
		return nil, err
	}
	defer session.EndSession(ctx)

	return session.WithTransaction(ctx, fn, opts...)
}
