package types

import relaytypes "eflow.local/new-api/relaykit/types"

type FileType = relaytypes.FileType

const (
	FileTypeImage = relaytypes.FileTypeImage
	FileTypeAudio = relaytypes.FileTypeAudio
	FileTypeVideo = relaytypes.FileTypeVideo
	FileTypeFile  = relaytypes.FileTypeFile
)

type TokenType = relaytypes.TokenType

const (
	TokenTypeTextNumber = relaytypes.TokenTypeTextNumber
	TokenTypeTokenizer  = relaytypes.TokenTypeTokenizer
	TokenTypeImage      = relaytypes.TokenTypeImage
)

type TokenCountMeta = relaytypes.TokenCountMeta
type FileMeta = relaytypes.FileMeta
type RequestMeta = relaytypes.RequestMeta

var NewFileMeta = relaytypes.NewFileMeta
var NewImageFileMeta = relaytypes.NewImageFileMeta
