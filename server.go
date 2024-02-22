package apigo

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/url"
	"path"

	brotli "github.com/anargu/gin-brotli"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/autotls"
	"github.com/gin-gonic/gin"
	"github.com/kardianos/osext"
	"github.com/quic-go/quic-go/http3"
	"github.com/tus/tusd/pkg/filestore"
	tusd "github.com/tus/tusd/pkg/handler"
	"github.com/zdypro888/idatabase"
	"go.mongodb.org/mongo-driver/bson"
	"golang.org/x/crypto/acme/autocert"
)

type Context = gin.Context

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

type TusdHandle func(ctx *gin.Context, reader io.Reader, info *tusd.FileInfo) (any, error)
type Server struct {
	App       *gin.Engine
	WithBSON  bool
	filestore *filestore.FileStore
	composer  *tusd.StoreComposer
}

func NewServer() *Server {
	app := gin.Default()
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	app.Use(cors.New(config))
	// app.Use(gzip.Gzip(gzip.DefaultCompression))
	app.Use(brotli.Brotli(brotli.DefaultCompression))
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

// TusdUpload handle upload request
// store: upload file store path
// path: upload request path. eg: /upload/
func (s *Server) TusdUpload(store, path string) error {
	if s.composer == nil || s.filestore == nil {
		s.filestore = &filestore.FileStore{Path: store}
		s.composer = tusd.NewStoreComposer()
		s.filestore.UseIn(s.composer)
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

type tusdRequest struct {
	URLs  []string `json:"urls"`
	Extra any      `json:"tag"`
}

type tusdResponse struct {
	Success map[string]any    `json:"success"`
	Failed  map[string]string `json:"failed"`
}

func (s *Server) tusdUploaded(ctx *gin.Context, handle TusdHandle) {
	var err error
	var request tusdRequest
	if err = ctx.BindJSON(&request); err != nil {
		ctx.AbortWithError(http.StatusBadRequest, err)
		return
	}
	response := &tusdResponse{}
	for _, URL := range request.URLs {
		var u *url.URL
		if u, err = url.Parse(URL); err == nil {
			fileID := path.Base(u.Path)
			var upload tusd.Upload
			if upload, err = s.filestore.GetUpload(context.Background(), fileID); err == nil {
				var reader io.Reader
				if reader, err = upload.GetReader(context.Background()); err == nil {
					var result any
					var fileInfo tusd.FileInfo
					if fileInfo, err = upload.GetInfo(context.Background()); err != nil {
						result, err = handle(ctx, reader, nil)
					} else {
						result, err = handle(ctx, reader, &fileInfo)
					}
					if err == nil {
						response.Success[URL] = result
					}
				}
			}
		}
		if err != nil {
			response.Failed[URL] = err.Error()
		}
	}
	ctx.JSON(http.StatusOK, response)
}

func (s *Server) TusdHandle(path string, handle TusdHandle) {
	s.App.POST(path, func(ctx *gin.Context) {
		s.tusdUploaded(ctx, handle)
	})
}

type tabulatorSort struct {
	Field string `json:"field"`
	Dir   string `json:"dir"`
}
type tabulatorFilter struct {
	Field string `json:"field"`
	Type  string `json:"type"`
	Value string `json:"value"`
}
type tabulatorRequest struct {
	Page    int               `json:"page"`   // page - the page number being requested
	Size    int               `json:"size"`   // size - the number of rows to a page (if paginationSize is set)
	Sorters []tabulatorSort   `json:"sort"`   // sorters - the first current sorters(if any)
	Filter  []tabulatorFilter `json:"filter"` // filter - an array of the current filters (if any)
}
type tabulatorResponse struct {
	LastPage int `bson:"last_page" json:"last_page"`
	Data     any `bson:"data" json:"data"`
}

// MarshalJSON 格式化
func (response *tabulatorResponse) MarshalJSON() ([]byte, error) {
	return bson.MarshalExtJSON(response, false, true)
}

// UnmarshalJSON 读取
func (response *tabulatorResponse) UnmarshalJSON(data []byte) error {
	return bson.UnmarshalExtJSON(data, false, response)
}

func TabulatorQuery[T any](ctx *gin.Context, collection string, selector any) {
	var pagination tabulatorRequest
	if err := ctx.BindJSON(&pagination); err != nil {
		ctx.AbortWithError(http.StatusInternalServerError, err)
	} else {
		query := &idatabase.QueryAny[T]{}
		query.Collection = collection
		query.Skip = (pagination.Page - 1) * pagination.Size
		query.Limit = pagination.Size
		for _, sort := range pagination.Sorters {
			sortValue := -1
			if sort.Dir == "asc" {
				sortValue = 1
			}
			query.Sorts = append(query.Sorts, bson.E{Key: sort.Field, Value: sortValue})
		}
		query.Check()
		total, err := query.Count()
		if err != nil {
			ctx.AbortWithError(http.StatusInternalServerError, err)
		} else if objects, err := query.Select(selector); err != nil {
			ctx.AbortWithError(http.StatusInternalServerError, err)
		} else {
			if objects == nil {
				objects = []T{}
			}
			ctx.JSON(http.StatusOK, &tabulatorResponse{LastPage: int(math.Ceil(float64(total) / float64(pagination.Size))), Data: objects})
		}
	}
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
