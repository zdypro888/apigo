package apigo

import (
	"fmt"
	"reflect"
	"testing"
)

type Service struct {
}

func (s *Service) ParamArgs(status int, anys ...int) (string, error) {
	return "", nil
}

func (s *Service) ParamArg(status int) (string, error) {
	return "", nil
}

func TestGRPC_Marshal(t *testing.T) {
	g := GRPC{Package: "apitest"}
	g.Marshal(reflect.TypeOf(&Service{}))
	fmt.Println(g.Proto3())
}
