package types

import relaytypes "github.com/QuantumNous/new-api/relaykit/types"

type FileSource = relaytypes.FileSource
type URLSource = relaytypes.URLSource
type Base64Source = relaytypes.Base64Source
type CachedFileData = relaytypes.CachedFileData

var NewURLFileSource = relaytypes.NewURLFileSource
var NewBase64FileSource = relaytypes.NewBase64FileSource
var NewFileSourceFromData = relaytypes.NewFileSourceFromData
var NewMemoryCachedData = relaytypes.NewMemoryCachedData
var NewDiskCachedData = relaytypes.NewDiskCachedData
