package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const defaultRepoSlug = "mingtian886/octopus"

// updateApiUrl 从 conf.Repo 推导，避免仓库地址在多处硬编码后失去同步。
// 发布流程只产出 Docker 镜像，不再上传二进制附件，所以这里只查询版本信息，
// 由前端引导用户拉取新镜像，不做进程内自更新。
var updateApiUrl = "https://api.github.com/repos/" + repoSlug() + "/releases/latest"

// repoSlug 从 conf.Repo 中解析 owner/repo；解析失败时回退到默认仓库。
func repoSlug() string {
	slug := strings.TrimSuffix(strings.TrimSpace(conf.Repo), "/")
	slug = strings.TrimPrefix(slug, "https://github.com/")
	slug = strings.TrimPrefix(slug, "http://github.com/")
	if slug == "" || strings.Contains(slug, "://") || strings.Count(slug, "/") != 1 {
		return defaultRepoSlug
	}
	return slug
}

type LatestInfo struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
	Message     string `json:"message"`
}

var github_pat = os.Getenv(strings.ToUpper(conf.APP_NAME) + "_GITHUB_PAT")

// doRequestWithFallback performs an HTTP GET request, first without proxy, then with proxy if failed.
func doRequestWithFallback(url string) ([]byte, error) {
	data, err := doRequest(url, false)
	if err == nil {
		return data, nil
	}
	log.Warnf("direct request failed, trying with proxy: %v", err)
	return doRequest(url, true)
}

func doRequest(url string, useProxy bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hc, err := client.GetHTTPClientSystemProxy(useProxy)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Debugf("new request failed: %v", err)
		return nil, err
	}

	if github_pat != "" {
		req.Header.Set("Authorization", "Bearer "+github_pat)
	}

	resp, err := hc.Do(req)
	if err != nil {
		log.Debugf("request failed: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	data, err := iolimit.ReadAll(resp.Body, iolimit.MetadataResponseMaxBytes())
	if err != nil {
		log.Debugf("read body failed: %v", err)
		return nil, err
	}
	return data, nil
}

func GetLatestInfo() (*LatestInfo, error) {
	body, err := doRequestWithFallback(updateApiUrl)
	if err != nil {
		return nil, err
	}

	var latestInfo LatestInfo
	if err := json.Unmarshal(body, &latestInfo); err != nil {
		log.Debugf("unmarshal body failed: %v", err)
		return nil, err
	}
	if latestInfo.Message != "" {
		return nil, fmt.Errorf("failed to get latest info: %s", latestInfo.Message)
	}
	return &latestInfo, nil
}
