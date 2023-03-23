package apigo

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"
	"sync"

	"github.com/iris-contrib/middleware/cors"
	"github.com/kataras/iris/v12"
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
	App      *iris.Application
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
		App: app,
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
	routers := s.App.HandleDir(requestPath, fsOrDir)
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
		s.App.WrapRouter(func(w http.ResponseWriter, r *http.Request, router http.HandlerFunc) {
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
func (s *Server) Start(domain, email, addr string) {
	manager := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domain),
		Cache:      autocert.DirCache("certs"),
		Email:      email,
	}
	s.App.Run(func(app *iris.Application) error {
		tlsConfig := &tls.Config{
			GetCertificate: manager.GetCertificate,
			NextProtos:     []string{"h2", "http/1.1", http3.NextProtoH3},
			MinVersion:     tls.VersionTLS13,
		}
		h12server := app.NewHost(&http.Server{Addr: addr, TLSConfig: tlsConfig})
		http3Server := &http3.Server{Handler: app, Addr: addr, TLSConfig: tlsConfig}
		var err error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			err = h12server.ListenAndServeTLS("", "")
			http3Server.Close()
		}()
		go func() {
			defer wg.Done()
			err = http3Server.ListenAndServe()
			h12server.Shutdown(context.Background())
		}()
		wg.Wait()
		return err
	}, iris.WithOptimizations)
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

func ReadMessage[T any](s *Server, ctx iris.Context) (*T, error) {
	if s.WithBSON {
		var bmsg bsonMessage[T]
		if err := ctx.ReadJSON(&bmsg); err != nil {
			return nil, err
		}
		return &bmsg.Data, nil
	} else {
		var msg T
		if err := ctx.ReadJSON(&msg); err != nil {
			return nil, err
		}
		return &msg, nil
	}
}

func (s *Server) ResponseError(ctx iris.Context, code int, err error) {
	if s.WithBSON {
		ctx.JSON(&messageBaseBSON{Code: code, Error: err.Error()})
	} else {
		ctx.JSON(&messageBase{Code: code, Error: err.Error()})
	}
}
func (s *Server) ResponseData(ctx iris.Context, data any) {
	if s.WithBSON {
		ctx.JSON(&messageBSON{Code: 0, Data: data})
	} else {
		ctx.JSON(&message[any]{Code: 0, Data: data})
	}
}

func (s *Server) HandleNotify(path string, handler func(ctx iris.Context)) {
	s.App.Get(path, handler)
}

func (s *Server) HandleRequest(path string, handler func(ctx iris.Context)) {
	s.App.Post(path, handler)
}
