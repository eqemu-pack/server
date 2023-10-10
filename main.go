package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type releaseJson struct {
	Name        string `json:"name"`
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	Prerelease  bool   `json:"prerelease"`
	Body        string `json:"body"`
}

var (
	client *http.Client
)

func main() {
	err := run()
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func run() error {
	client = &http.Client{
		Timeout: 10 * time.Second,
	}

	// first, get a list of releases
	releases, err := githubReleases()
	if err != nil {
		return fmt.Errorf("githubReleases: %w", err)
	}

	var latestUnstableRelease *releaseJson
	var latestStableRelease *releaseJson
	var fallbackRelease *releaseJson
	var lastReleasePublishDate time.Time

	for _, release := range releases {
		if release.Prerelease {
			fmt.Println("Skipping", release.TagName, "since it's a prerelease")
			continue
		}
		if latestUnstableRelease == nil {
			latestUnstableRelease = release
		}
		// convert PublishedAt 2023-09-18T17:19:56Z to time.Time
		publishedAt, err := time.Parse(time.RFC3339, release.PublishedAt)
		if err != nil {
			return fmt.Errorf("parse published at: %w", err)
		}

		if !lastReleasePublishDate.IsZero() &&
			lastReleasePublishDate.Add(-3*24*time.Hour).Before(publishedAt) {
			fmt.Printf("Skipping %s, too close to last release (last: %s this: %s)\n", release.TagName, lastReleasePublishDate, publishedAt)
			lastReleasePublishDate = publishedAt
			continue
		}

		if fallbackRelease == nil &&
			time.Since(publishedAt) > 30*24*time.Hour {
			fallbackRelease = release
			fmt.Println("Setting fallback release to", release.TagName, "since it's 30 days old")
		}
		fmt.Println("Checking release", release.TagName)
		lastReleasePublishDate = publishedAt

		// if stable release is less than a week old, skip it
		if time.Since(publishedAt) < 7*24*time.Hour {
			fmt.Printf("Skipping %s, too new\n", release.TagName)
			continue
		}
		//fallback release is 30 days old release

		if !strings.Contains(release.Body, "Fix") {
			fmt.Printf("Skipping %s, no fixes\n", release.TagName)
			continue
		}

		releaseTag := strings.ReplaceAll(release.TagName, "v", "")
		errorCount, err := errorCount(releaseTag)
		if err != nil {
			return fmt.Errorf("errorCount: %w", err)
		}

		if errorCount > 0 {
			fmt.Printf("%s has %d errors, skipping\n", releaseTag, errorCount)
			continue
		}

		latestStableRelease = release
		break
	}

	if latestStableRelease == nil {
		if fallbackRelease == nil {
			return fmt.Errorf("no releases found")
		}
		fmt.Println("No releases found, using fallback release")
		latestStableRelease = fallbackRelease
	}

	err = os.MkdirAll("bin", 0755)
	if err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	fmt.Println("Latest unstable release:", latestUnstableRelease.TagName)
	fmt.Println("Latest stable release:", latestStableRelease.TagName)
	err = os.WriteFile("bin/latest.txt", []byte(latestUnstableRelease.TagName), 0644)
	if err != nil {
		return fmt.Errorf("write latest.txt: %w", err)
	}

	err = os.WriteFile("bin/stable.txt", []byte(latestStableRelease.TagName), 0644)
	if err != nil {
		return fmt.Errorf("write stable.txt: %w", err)
	}

	return nil
}

func githubReleases() ([]*releaseJson, error) {
	resp, err := client.Get("https://api.github.com/repos/eqemu/server/releases")
	if err != nil {
		return nil, fmt.Errorf("get releases: %w", err)
	}
	defer resp.Body.Close()

	// read resp body to buf
	payloads := []*releaseJson{}
	err = json.NewDecoder(resp.Body).Decode(&payloads)
	if err != nil {
		return nil, fmt.Errorf("decode releases: %w", err)
	}

	releases := append([]*releaseJson{}, payloads...)

	return releases, nil
}

func errorCount(tag string) (int, error) {
	resp, err := client.Get(fmt.Sprintf("http://spire.akkadius.com/api/v1/analytics/server-crash-reports?version=%s", tag))
	if err != nil {
		return 0, fmt.Errorf("get error count: %w", err)
	}
	defer resp.Body.Close()

	type errorCountJson struct {
		Id              int    `json:"id"`
		ServerName      string `json:"server_name"`
		ServerShortName string `json:"server_short_name"`
		ServerVersion   string `json:"server_version"`
	}

	// read resp body to buf
	payloads := []*errorCountJson{}
	err = json.NewDecoder(resp.Body).Decode(&payloads)
	if err != nil {
		return 0, fmt.Errorf("decode error count: %w", err)
	}

	servers := make(map[string]string)
	count := 0
	for _, payload := range payloads {
		if _, ok := servers[payload.ServerName]; ok {
			continue
		}
		servers[payload.ServerName] = payload.ServerName
		count++
	}

	return count, nil

}
