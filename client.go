package apigo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zdypro888/net"
	"go.mongodb.org/mongo-driver/bson"
)

type Client struct {
	client   *net.HTTP
	host     string
	WithBSON bool
}

func (c *Client) BuildURL(p string) string {
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	if strings.HasPrefix(p, "/") {
		return c.host + p
	}
	return c.host + "/" + p
}

func NewClient(host string) *Client {
	client := &Client{
		host:     host,
		WithBSON: true,
	}
	if strings.HasPrefix(host, "http://") {
		client.client = net.NewHTTP(nil)
	} else {
		client.client = net.NewHTTP3()
	}
	return client
}

func doRequest(c *Client, path string, method string, request any, response any) error {
	var err error
	var data []byte
	if request == nil {
		data = nil
	} else if raw, ok := request.([]byte); ok {
		data = raw
	} else if c.WithBSON {
		if data, err = bson.MarshalExtJSON(request, false, true); err != nil {
			return err
		}
	} else {
		if data, err = json.Marshal(request); err != nil {
			return err
		}
	}
	var res *net.Response
	if res, err = c.client.RequestMethod(context.Background(), c.BuildURL(path), method, nil, net.NewReader(data)); err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return res
	}
	if data, err = res.Data(); err != nil {
		return err
	}
	if c.WithBSON {
		if err = bson.UnmarshalExtJSON(data, false, response); err != nil {
			return err
		}
	} else {
		if err = json.Unmarshal(data, response); err != nil {
			return err
		}
	}
	return nil
}

func Notify(c *Client, path string, method string, body any) error {
	var msg messageBase
	if err := doRequest(c, path, method, body, &msg); err != nil {
		return err
	}
	if msg.Code != 0 {
		return errors.New(msg.Error)
	}
	return nil
}

func Request[T any](c *Client, path string, method string, body any) (*T, error) {
	var msg message[*T]
	if err := doRequest(c, path, method, body, &msg); err != nil {
		return nil, err
	}
	if msg.Code != 0 {
		return nil, errors.New(msg.Error)
	}
	return msg.Data, nil
}
