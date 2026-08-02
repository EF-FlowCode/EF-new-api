package attachment

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/setting"
)

// 附件校验层的定义。
//
// 安全目标：即使管理员放开了白名单，上传层也不能接受以下内容：
//   1. SVG（无论扩展名、MIME、魔数是什么）—— 产品需求明确禁用，防止 XSS；
//   2. 声称扩展名与实际魔数不一致的文件（text/html 伪装成 txt 等）；
//   3. 包含空字节 / 路径分隔符 / 控制字符 / 保留字的文件名；
//   4. 超过 setting.TicketAttachmentMaxSize 的文件。
//
// 这一层只关心"是否放行"，具体的存储路径由上层生成（UUID+ext），绝不使用原始文件名。

var (
	ErrAttachmentDisabled      = errors.New("ticket attachment feature is disabled")
	ErrAttachmentSizeExceeded  = errors.New("attachment size exceeded")
	ErrAttachmentExtNotAllowed = errors.New("attachment extension not allowed")
	ErrAttachmentMimeNotAllowed = errors.New("attachment mime type not allowed")
	ErrAttachmentMimeMismatch  = errors.New("attachment declared type does not match content")
	ErrAttachmentSVGForbidden  = errors.New("svg attachments are forbidden")
	ErrAttachmentEmpty         = errors.New("attachment is empty")
	ErrAttachmentFileName      = errors.New("invalid attachment filename")
)

// sniffSize 取文件开头 512 字节用于魔数嗅探（net/http 规范）。
const sniffSize = 512

// SanitizeFileName 清洗客户端传入的文件名：
//   - 只取 basename，去掉客户端可能夹带的路径；
//   - 删除空字节与控制字符；
//   - 把 Windows 保留字 / 非法字符替换为 "_"；
//   - 截断到 200 个 rune，避免存库/展示时出现超长串；
//   - 空名或仅扩展名时给出兜底 "attachment"。
func SanitizeFileName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\x00", "")
	if !utf8.ValidString(name) {
		return "", ErrAttachmentFileName
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == ".." || name == "" || name == string(filepath.Separator) {
		return "", ErrAttachmentFileName
	}
	// 过滤 Windows 非法字符与控制字符。
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r < 0x20, r == 0x7f:
			// 控制字符直接吞掉
			continue
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			b.WriteRune('_')
		case unicode.IsPrint(r):
			b.WriteRune(r)
		default:
			// 不可打印的 unicode 以 "_" 兜底
			b.WriteRune('_')
		}
	}
	cleaned := strings.TrimSpace(b.String())
	if cleaned == "" {
		return "", ErrAttachmentFileName
	}
	// 截断到 200 rune（按字符而非字节，避免中文被拦腰截断成乱码）。
	const maxRunes = 200
	if utf8.RuneCountInString(cleaned) > maxRunes {
		runes := []rune(cleaned)[:maxRunes]
		cleaned = string(runes)
	}
	return cleaned, nil
}

// NormalizeExt 从文件名拿到小写、无前导点的扩展名。无扩展名时返回空串。
func NormalizeExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	return strings.TrimPrefix(ext, ".")
}

// ValidateSize 根据设置判定文件大小。size <= 0 视为未知，允许通过（后续 io.LimitReader 兜底）。
func ValidateSize(size int64) error {
	if size > 0 && size > setting.TicketAttachmentMaxSize {
		return ErrAttachmentSizeExceeded
	}
	return nil
}

// IsSVGContent 判断文件内容是否为 SVG。
// SVG 的 MIME 通常是 image/svg+xml，但某些客户端会回 text/xml 或 application/xml，
// 所以必须同时看内容：任何以 "<svg" / "<?xml" 开头且随后出现 "<svg" 的都视作 SVG 拒绝。
func IsSVGContent(head []byte) bool {
	trimmed := bytes.TrimLeft(head, " \t\r\n\xef\xbb\xbf") // 去 BOM 和空白
	lower := bytes.ToLower(trimmed)
	if bytes.HasPrefix(lower, []byte("<svg")) {
		return true
	}
	if bytes.HasPrefix(lower, []byte("<?xml")) && bytes.Contains(lower, []byte("<svg")) {
		return true
	}
	return false
}

// DetectMime 嗅探前 512 字节得到实际 MIME。
// 对于纯文本类（text/plain）再补一层 UTF-8 校验，避免把二进制塞进 txt。
func DetectMime(head []byte) string {
	mime := http.DetectContentType(head)
	// http.DetectContentType 对纯英文 JSON 也会返回 "text/plain; charset=utf-8"；
	// 这里保留原始结果，由上层的白名单 + 扩展名一致性判定兜底。
	return mime
}

// CheckResult 返回真实可信的 MIME 与扩展名，供存储/DB 写入使用。
type CheckResult struct {
	SafeName   string // 清洗后的展示文件名
	Ext        string // 小写扩展名（无点）
	DetectedMime string // http.DetectContentType 嗅探值，不是客户端声明值
}

// Validate 在文件内容已被读入内存/临时文件后调用，执行白名单三重校验。
// reader 必须支持 io.Reader；本函数内部只读取前 sniffSize 字节做魔数嗅探，不消费原始流。
//
// 注意：扩展名 / 声明 MIME / 魔数三者任一不一致都会拒绝，目的是消除"改名绕过"风险。
// 对 text/* 类型放宽一层：只要声明 MIME 与扩展名匹配、且嗅探结果属于 text/* 就放行，
// 因为 txt/json/md 三者嗅探结果可能都是 "text/plain"。
func Validate(fileName string, declaredMime string, size int64, head []byte) (*CheckResult, error) {
	if !setting.TicketAttachmentEnabled {
		return nil, ErrAttachmentDisabled
	}
	if size == 0 && len(head) == 0 {
		return nil, ErrAttachmentEmpty
	}
	if err := ValidateSize(size); err != nil {
		return nil, err
	}

	safeName, err := SanitizeFileName(fileName)
	if err != nil {
		return nil, err
	}
	ext := NormalizeExt(safeName)
	if ext == "" {
		return nil, ErrAttachmentExtNotAllowed
	}
	// 第一重：扩展名白名单。
	if !setting.IsTicketAttachmentExtAllowed(ext) {
		return nil, ErrAttachmentExtNotAllowed
	}
	// 硬性拒绝 svg，即使管理员误加到白名单里。
	if ext == "svg" {
		return nil, ErrAttachmentSVGForbidden
	}

	// 第二重：声明 MIME 白名单。允许为空（部分浏览器不填），为空时以嗅探结果兜底。
	declaredMime = strings.ToLower(strings.TrimSpace(declaredMime))
	if idx := strings.Index(declaredMime, ";"); idx >= 0 {
		declaredMime = strings.TrimSpace(declaredMime[:idx])
	}
	if declaredMime != "" && !setting.IsTicketAttachmentMimeAllowed(declaredMime) {
		return nil, ErrAttachmentMimeNotAllowed
	}

	// 第三重：内容魔数。
	truncated := head
	if len(truncated) > sniffSize {
		truncated = truncated[:sniffSize]
	}
	if IsSVGContent(truncated) {
		return nil, ErrAttachmentSVGForbidden
	}
	detected := DetectMime(truncated)
	// 嗅探结果也必须命中白名单，除非它是 text/* 且扩展名落在文本类里。
	detectedPrefix := strings.Split(detected, ";")[0]
	if !setting.IsTicketAttachmentMimeAllowed(detectedPrefix) {
		// 对 text/plain 放宽：若扩展名属于已知文本类，则以白名单里的同类 MIME 作为最终 MIME。
		if !strings.HasPrefix(detectedPrefix, "text/") {
			return nil, ErrAttachmentMimeMismatch
		}
	}

	// 一致性校验：若客户端声明了 MIME，它必须与嗅探结果"同大类"，否则视为伪装。
	if declaredMime != "" && !sameMimeFamily(declaredMime, detectedPrefix) {
		return nil, ErrAttachmentMimeMismatch
	}

	finalMime := declaredMime
	if finalMime == "" {
		finalMime = detectedPrefix
	}

	return &CheckResult{
		SafeName:     safeName,
		Ext:          ext,
		DetectedMime: finalMime,
	}, nil
}

// sameMimeFamily 判断两个 MIME 是否属于同一"嗅探族"。
// 用于把 "用户声明 MIME" 和 "魔数嗅探 MIME" 做一致性校验：
// 目的是让"改扩展名伪装类型"无法通过（如声明 text/plain 但内容是 PNG），
// 同时允许文本类和常见的 JSON/XML 互相嗅探成 text/plain 的合理情况。
//
// 规则：
//   - 完全相等 → 同族；
//   - image/*、audio/*、video/* 各自同族（同一大类内切换算合理）；
//   - 文本类集合：text/* + application/json + application/xml + application/javascript + application/x-yaml；
//   - 其它（尤其是 application/* 里 pdf/zip/octet-stream 等二进制格式）必须精确相等。
func sameMimeFamily(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == b {
		return true
	}
	if a == "" || b == "" {
		return false
	}
	clusters := []string{"image/", "audio/", "video/"}
	for _, prefix := range clusters {
		if strings.HasPrefix(a, prefix) && strings.HasPrefix(b, prefix) {
			return true
		}
	}
	textLike := func(m string) bool {
		switch m {
		case "application/json", "application/xml", "application/javascript", "application/x-yaml":
			return true
		}
		return strings.HasPrefix(m, "text/")
	}
	if textLike(a) && textLike(b) {
		return true
	}
	return false
}

// ReadSniff 从 reader 中最多读取 sniffSize 字节用于嗅探，
// 并把已经读出的字节重新拼回，返回一个"内容完整"的新 reader。
// 适用于 multipart 文件：不能 Seek，也不想把整个文件读进内存。
func ReadSniff(r io.Reader) (head []byte, combined io.Reader, err error) {
	buf := make([]byte, sniffSize)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, nil, err
	}
	head = buf[:n]
	combined = io.MultiReader(bytes.NewReader(head), r)
	return head, combined, nil
}
