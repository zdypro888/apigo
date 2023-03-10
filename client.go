package apigo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zdypro888/net"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type response[T any] struct {
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
	Data  T      `json:"data,omitempty"`
}

type Client struct {
	client   *net.HTTP
	host     string
	WithBSON bool
}

func NewClient(host string) *Client {
	client := &Client{
		client: net.NewHTTP3(),
		host:   host,
	}
	return client
}

func Request[T any](c *Client, url string, method string, body any) (*T, error) {
	var err error
	var data []byte
	if body == nil {
		data = nil
	} else if raw, ok := body.([]byte); ok {
		data = raw
	} else if c.WithBSON {
		if data, err = bson.MarshalExtJSON(body, false, true); err != nil {
			return nil, err
		}
	} else {
		if data, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	var res *net.Response
	if res, err = c.client.RequestMethod(context.Background(), url, method, nil, data); err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, res
	}
	if data, err = res.Data(); err != nil {
		return nil, err
	}
	var result response[*T]
	if c.WithBSON {
		if err = bson.UnmarshalExtJSON(data, false, &result); err != nil {
			return nil, err
		}
	} else {
		if err = json.Unmarshal(data, &result); err != nil {
			return nil, err
		}
	}
	if result.Code != 0 {
		return nil, errors.New(result.Error)
	}
	return result.Data, nil
}

func (c *Client) Notify(url string, data ...any) error {
	var err error
	if len(data) == 0 {
		_, err = Request[any](c, url, http.MethodGet, nil)
	} else if data[0] == nil {
		_, err = Request[any](c, url, http.MethodPost, []byte("{}"))
	} else {
		_, err = Request[any](c, url, http.MethodPost, data[0])
	}
	return err
}

func Notify(c *Client, url string, data ...any) error {
	return c.Notify(url, data...)
}

type BSONObject[T any] struct {
	ID     primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	Object T                  `bson:",inline" json:",inline"`
}
