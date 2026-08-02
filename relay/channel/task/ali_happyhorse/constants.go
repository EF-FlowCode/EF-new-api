package ali_happyhorse

var ModelList = []string{
	"happyhorse-1.0-t2v",        // 文生视频
	"happyhorse-1.0-i2v",        // 图生视频（首帧）
	"happyhorse-1.0-r2v",        // 参考图生视频（保留主体）
	"happyhorse-1.0-video-edit", // 视频编辑
}

var ChannelName = "ali_happyhorse"

const (
	MinDuration = 3
	MaxDuration = 15
)
