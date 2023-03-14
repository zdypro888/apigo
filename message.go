package apigo

type messageBase struct {
	Code  int    `json:"code" bson:"code"`
	Error string `json:"error,omitempty" bson:"error,omitempty"`
}

type message[T any] struct {
	Code  int    `json:"code" bson:"code"`
	Error string `json:"error,omitempty" bson:"error,omitempty"`
	Data  T      `json:"data,omitempty" bson:"data,omitempty"`
}
