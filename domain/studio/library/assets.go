package library

import "errors"

const MaxAssetBytes = 2 << 20

var (
	ErrUnknownAsset  = errors.New("library: no picture item has this id")
	ErrAssetMismatch = errors.New("library: the file is not the picture of this item")
)
