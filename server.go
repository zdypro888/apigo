package apigo

import (
	"net/http"
	"path"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/autotls"
	"github.com/gin-gonic/gin"
	"github.com/kardianos/osext"
	"github.com/quic-go/quic-go/http3"
	"github.com/tus/tusd/pkg/filestore"
	tusd "github.com/tus/tusd/pkg/handler"
	"go.mongodb.org/mongo-driver/bson"
	"golang.org/x/crypto/acme/autocert"
)

type messageBaseBSON messageBase

// MarshalJSON 格式化
func (msg *messageBaseBSON) MarshalJSON() ([]byte, error) {
	return bson.MarshalExtJSON(msg, false, true)
}

// UnmarshalJSON 读取
func (msg *messageBaseBSON) UnmarshalJSON(data []byte) error {
	return bson.UnmarshalExtJSON(data, false, msg)
}

type messageBSON message[any]

// MarshalJSON 格式化
func (msg *messageBSON) MarshalJSON() ([]byte, error) {
	return bson.MarshalExtJSON(msg, false, true)
}

// UnmarshalJSON 读取
func (msg *messageBSON) UnmarshalJSON(data []byte) error {
	return bson.UnmarshalExtJSON(data, false, msg)
}

type Server struct {
	App      *gin.Engine
	composer *tusd.StoreComposer
	WithBSON bool
}

func NewServer() *Server {
	app := gin.Default()
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	app.Use(cors.New(config))
	return &Server{
		App:      app,
		WithBSON: true,
	}
}

// Static add Cross-Origin-Opener-Policy: same-origin and Cross-Origin-Embedder-Policy: require-corp to all routers
func (s *Server) Static(relativePath string, root string) {
	crossHandle := func(ctx *gin.Context) {
		ctx.Header("Cross-Origin-Embedder-Policy", "require-corp")
		ctx.Header("Cross-Origin-Opener-Policy", "same-origin")
		ctx.Next()
	}
	router := s.App.Static(relativePath, root)
	router.Use(crossHandle)
}

// HandleUpload handle upload request
// store: upload file store path
// path: upload request path. eg: /upload/
func (s *Server) HandleUpload(store, path string) error {
	if s.composer == nil {
		fs := &filestore.FileStore{Path: store}
		s.composer = tusd.NewStoreComposer()
		fs.UseIn(s.composer)
	}
	handler, err := tusd.NewHandler(tusd.Config{
		BasePath:      path,
		StoreComposer: s.composer,
	})
	if err == nil {
		stripHandle := http.StripPrefix(path, handler)
		s.App.Use(gin.WrapH(stripHandle))
	}
	return err
}

// Start start server
// cert: tls cert file path
// key: tls key file path
// addr: listen address. eg: :https
func (s *Server) Start(domain, email, addr string) error {
	folder, err := osext.ExecutableFolder()
	if err != nil {
		return err
	}
	m := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domain),
		Cache:      autocert.DirCache(path.Join(folder, "certs")),
	}
	http3Server := &http3.Server{Handler: s.App, Addr: addr, TLSConfig: m.TLSConfig()}
	go http3Server.ListenAndServe()
	go autotls.RunWithManager(s.App, &m)
	return nil
}

type bsonMessage[T any] struct {
	Data T `json:",inline" bson:",inline"`
}

// MarshalJSON 格式化
func (bmsg *bsonMessage[T]) MarshalJSON() ([]byte, error) {
	return bson.MarshalExtJSON(bmsg, false, true)
}

// UnmarshalJSON 读取
func (bmsg *bsonMessage[T]) UnmarshalJSON(data []byte) error {
	return bson.UnmarshalExtJSON(data, false, bmsg)
}

func ReadMessage[T any](s *Server, ctx *gin.Context) (*T, error) {
	if s.WithBSON {
		var bmsg bsonMessage[T]
		if err := ctx.BindJSON(&bmsg); err != nil {
			return nil, err
		}
		return &bmsg.Data, nil
	} else {
		var msg T
		if err := ctx.BindJSON(&msg); err != nil {
			return nil, err
		}
		return &msg, nil
	}
}

func (s *Server) ResponseError(ctx *gin.Context, code int, err error) {
	if s.WithBSON {
		ctx.JSON(http.StatusOK, &messageBaseBSON{Code: code, Error: err.Error()})
	} else {
		ctx.JSON(http.StatusOK, &messageBase{Code: code, Error: err.Error()})
	}
}
func (s *Server) ResponseData(ctx *gin.Context, data any) {
	if s.WithBSON {
		ctx.JSON(http.StatusOK, &messageBSON{Code: 0, Data: data})
	} else {
		ctx.JSON(http.StatusOK, &message[any]{Code: 0, Data: data})
	}
}

func (s *Server) HandleGet(path string, handler func(ctx *gin.Context)) {
	s.App.GET(path, handler)
}

func (s *Server) HandlePost(path string, handler func(ctx *gin.Context)) {
	s.App.POST(path, handler)
}
