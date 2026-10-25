package knowledge

import (
	"fmt"
	"net/url"
	"strings"
)

func AliBatchBase(raw string) (string, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" || (u.Hostname() != "dashscope.aliyuncs.com" && !strings.HasSuffix(u.Hostname(), ".cn-beijing.maas.aliyuncs.com")) {
		return "", fmt.Errorf("native embedding Batch File requires a verified Bailian Beijing endpoint")
	}
	if u.Path != "" && u.Path != "/compatible-mode" && u.Path != "/compatible-mode/v1" {
		return "", fmt.Errorf("unsupported Bailian Batch base path")
	}
	u.Path, u.RawPath = "/compatible-mode/v1", ""
	return u.String(), nil
}
