package apigo

import (
	"net/http"
	"strings"

	"github.com/iris-contrib/middleware/cors"
	"github.com/kataras/iris/v12"
	"github.com/quic-go/quic-go/http3"
	"github.com/tus/tusd/pkg/filestore"
	tusd "github.com/tus/tusd/pkg/handler"
	"go.mongodb.org/mongo-driver/bson"
)

type Response struct {
	Code  int    `json:"code" bson:"code"`
	Error string `json:"error,omitempty" bson:"error,omitempty"`
	Data  any    `json:"data,omitempty" bson:"data,omitempty"`
}

type ResponseBSON Response

// MarshalJSON 格式化
func (result *ResponseBSON) MarshalJSON() ([]byte, error) {
	return bson.MarshalExtJSON(result, false, true)
}

// UnmarshalJSON 读取
func (result *ResponseBSON) UnmarshalJSON(data []byte) error {
	return bson.UnmarshalExtJSON(data, false, result)
}

type Server struct {
	app      *iris.Application
	store    *filestore.FileStore
	WithBSON bool
}

func NewServer() *Server {
	app := iris.New()
	crs := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})
	app.UseRouter(crs)
	app.AllowMethods(iris.MethodOptions)
	return &Server{
		app: app,
	}
}

// HandleDir add Cross-Origin-Opener-Policy: same-origin and Cross-Origin-Embedder-Policy: require-corp to all routers
// fsOrDir can be iris.Dir or http.FileSystem
func (s *Server) HandleDir(requestPath string, fsOrDir any) {
	crossHandle := func(ctx iris.Context) {
		ctx.Header("Cross-Origin-Embedder-Policy", "require-corp")
		ctx.Header("Cross-Origin-Opener-Policy", "same-origin")
		ctx.Next()
	}
	routers := s.app.HandleDir(requestPath, fsOrDir)
	for _, router := range routers {
		router.Use(crossHandle)
	}
}

// HandleUpload handle upload request
// store: upload file store path
// path: upload request path. eg: /upload/
func (s *Server) HandleUpload(store, path string) error {
	s.store = &filestore.FileStore{Path: store}
	composer := tusd.NewStoreComposer()
	s.store.UseIn(composer)
	handler, err := tusd.NewHandler(tusd.Config{
		BasePath:      path,
		StoreComposer: composer,
	})
	if err == nil {
		stripHandle := http.StripPrefix(path, handler)
		s.app.WrapRouter(func(w http.ResponseWriter, r *http.Request, router http.HandlerFunc) {
			if strings.HasPrefix(r.URL.Path, path) {
				stripHandle.ServeHTTP(w, r)
			} else {
				router.ServeHTTP(w, r)
			}
		})
	}
	return err
}

// Start start server
// cert: tls cert file path
// key: tls key file path
// addr: listen address. eg: :https
func (s *Server) Start(cert, key, addr string) {
	go s.app.Run(iris.Raw(func() error {
		hErr := make(chan error)
		qErr := make(chan error)
		go func() {
			hErr <- s.app.NewHost(&http.Server{Addr: addr}).ListenAndServeTLS(cert, key)
		}()
		go func() {
			qErr <- http3.ListenAndServeQUIC(addr, cert, key, s.app)
		}()
		select {
		case err := <-hErr:
			return err
		case err := <-qErr:
			return err
		}
	}), iris.WithOptimizations)
}

func (s *Server) ResponseError(ctx iris.Context, code int, err error) {
	if s.WithBSON {
		ctx.JSON(&ResponseBSON{Code: code, Error: err.Error()})
	} else {
		ctx.JSON(&Response{Code: code, Error: err.Error()})
	}
}
func (s *Server) ResponseData(ctx iris.Context, data any) {
	if s.WithBSON {
		ctx.JSON(&ResponseBSON{Code: 0, Data: data})
	} else {
		ctx.JSON(&Response{Code: 0, Data: data})
	}
}

func (s *Server) HandleNotify(path string, handler func(ctx iris.Context)) {
	s.app.Get(path, handler)
}

func (s *Server) HandleRequest(path string, handler func(ctx iris.Context)) {
	s.app.Post(path, handler)
}
