package apigo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zdypro888/net"
	"go.mongodb.org/mongo-driver/bson"
)

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

func doRequest(c *Client, url string, method string, request any, response any) error {
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
	if res, err = c.client.RequestMethod(context.Background(), url, method, nil, data); err != nil {
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

func Notify(c *Client, url string, method string, body any) error {
	var result responseBase
	if err := doRequest(c, url, method, body, &result); err != nil {
		return err
	}
	if result.Code != 0 {
		return errors.New(result.Error)
	}
	return nil
}

func Request[T any](c *Client, url string, method string, body any) (*T, error) {
	var result response[*T]
	if err := doRequest(c, url, method, body, &result); err != nil {
		return nil, err
	}
	if result.Code != 0 {
		return nil, errors.New(result.Error)
	}
	return result.Data, nil
}
