package apigo

import (
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type BSONObject[T any] struct {
	ID     primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	Object T                  `bson:",inline" json:",inline"`
}

// MarshalJSON 格式化
func (bo *BSONObject[T]) MarshalJSON() ([]byte, error) {
	return bson.MarshalExtJSON(bo, false, true)
}

// UnmarshalJSON 读取
func (bo *BSONObject[T]) UnmarshalJSON(data []byte) error {
	return bson.UnmarshalExtJSON(data, false, bo)
}

type BSONData[T any] struct {
	Data T `json:",inline" bson:",inline"`
}

// MarshalJSON 格式化
func (bd *BSONData[T]) MarshalJSON() ([]byte, error) {
	return bson.MarshalExtJSON(bd, false, true)
}

// UnmarshalJSON 读取
func (bd *BSONData[T]) UnmarshalJSON(data []byte) error {
	return bson.UnmarshalExtJSON(data, false, bd)
}
