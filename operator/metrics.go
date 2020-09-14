package operator

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	gcsLinkToken  = "gcsweb"
	storagePrefix = "https://storage.googleapis.com"
	promTarPath   = "metrics/prometheus.tar"
	extraPath     = "gather-extra"
	e2ePrefix     = "e2e"
)

func getLinksFromURL(url string) ([]string, error) {
	links := []string{}

	var netClient = &http.Client{
		Timeout: time.Second * 10,
	}
	resp, err := netClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	z := html.NewTokenizer(resp.Body)
	for {
		tt := z.Next()

		switch {
		case tt == html.ErrorToken:
			// End of the document, we're done
			return links, nil
		case tt == html.StartTagToken:
			t := z.Token()

			isAnchor := t.Data == "a"
			if isAnchor {
				for _, a := range t.Attr {
					if a.Key == "href" {
						links = append(links, a.Val)
						break
					}
				}
			}
		}
	}
}

func getTarURLFromProw(baseURL string, gcsPrefix string) (string, error) {
	// Is it a direct prom tarball link?
	if strings.HasSuffix(baseURL, promTarPath) {
		return baseURL, nil
	}

	// Get a list of links on prow page
	prowToplinks, err := getLinksFromURL(baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to find links at %s: %w", prowToplinks, err)
	}
	if len(prowToplinks) == 0 {
		return "", fmt.Errorf("no links found at %s", baseURL)
	}
	gcsTempURL := ""
	for _, link := range prowToplinks {
		if strings.Contains(link, gcsLinkToken) {
			gcsTempURL = link
			break
		}
	}
	if gcsTempURL == "" {
		return "", fmt.Errorf("failed to find GCS link in %v", prowToplinks)
	}

	gcsURL, err := url.Parse(gcsTempURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse GCS URL %s: %w", gcsTempURL, err)
	}

	// Check that 'artifacts' folder is present
	gcsToplinks, err := getLinksFromURL(gcsURL.String())
	if err != nil {
		return "", fmt.Errorf("failed to fetch top-level GCS link at %s: %w", gcsURL, err)
	}
	if len(gcsToplinks) == 0 {
		return "", fmt.Errorf("no top-level GCS links at %s found", gcsURL)
	}
	tmpArtifactsURL := ""
	for _, link := range gcsToplinks {
		if strings.HasSuffix(link, "artifacts/") {
			tmpArtifactsURL = gcsPrefix + link
			break
		}
	}
	if tmpArtifactsURL == "" {
		return "", fmt.Errorf("failed to find artifacts link in %v", gcsToplinks)
	}
	artifactsURL, err := url.Parse(tmpArtifactsURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse artifacts link %s: %w", tmpArtifactsURL, err)
	}

	// Get a list of folders in find ones which contain e2e
	artifactLinksToplinks, err := getLinksFromURL(artifactsURL.String())
	if err != nil {
		return "", fmt.Errorf("failed to fetch artifacts link at %s: %w", gcsURL, err)
	}
	if len(artifactLinksToplinks) == 0 {
		return "", fmt.Errorf("no artifact links at %s found", gcsURL)
	}
	tmpE2eURL := ""
	for _, link := range artifactLinksToplinks {
		linkSplitBySlash := strings.Split(link, "/")
		lastPathSegment := linkSplitBySlash[len(linkSplitBySlash)-1]
		if len(lastPathSegment) == 0 {
			lastPathSegment = linkSplitBySlash[len(linkSplitBySlash)-2]
		}
		if strings.Contains(lastPathSegment, e2ePrefix) {
			tmpE2eURL = gcsPrefix + link
			break
		}
	}
	if tmpE2eURL == "" {
		return "", fmt.Errorf("failed to find e2e link in %v", artifactLinksToplinks)
	}
	e2eURL, err := url.Parse(tmpE2eURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse e2e link %s: %w", tmpE2eURL, err)
	}

	// Support new-style jobs
	e2eToplinks, err := getLinksFromURL(e2eURL.String())
	if err != nil {
		return "", fmt.Errorf("failed to fetch artifacts link at %s: %w", e2eURL, err)
	}
	if len(e2eToplinks) == 0 {
		return "", fmt.Errorf("no top links at %s found", e2eURL)
	}
	for _, link := range e2eToplinks {
		linkSplitBySlash := strings.Split(link, "/")
		lastPathSegment := linkSplitBySlash[len(linkSplitBySlash)-1]
		if len(lastPathSegment) == 0 {
			lastPathSegment = linkSplitBySlash[len(linkSplitBySlash)-2]
		}
		if lastPathSegment == extraPath {
			tmpE2eURL = gcsPrefix + link
			e2eURL, err = url.Parse(tmpE2eURL)
			if err != nil {
				return "", fmt.Errorf("failed to parse e2e link %s: %v", tmpE2eURL, err)
			}
			break
		}
	}

	gcsMetricsURL := fmt.Sprintf("%s%s", e2eURL.String(), promTarPath)
	tempMetricsURL := strings.Replace(gcsMetricsURL, gcsPrefix+"/gcs", storagePrefix, -1)
	expectedMetricsURL, err := url.Parse(tempMetricsURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse metrics link %s: %w", tempMetricsURL, err)
	}
	return expectedMetricsURL.String(), nil
}
