package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Aryan-mor/sushi-study/be/internal/adapter/tg"
	"github.com/Aryan-mor/sushi-study/be/internal/core/catalog"
)

func (b *bot) handle() string {
	if b.client == nil {
		return ""
	}
	return b.client.Username()
}

func homeKeyboard() tg.InlineKeyboard {
	return homeKeyboardQuery("")
}

func homeKeyboardQuery(query string) tg.InlineKeyboard {
	query = strings.TrimSpace(query)
	if query == "" {
		query = " "
	} else {
		query = truncateRunes(query, inlineQueryMax)
	}
	return tg.InlineKeyboard{Rows: [][]tg.InlineButton{{
		{Text: searchBtn, SwitchInlineQueryCurrentChat: query},
	}}}
}

func titleActionRows(kind catalog.Kind, tmdbID uint64, mainUser string) [][]tg.InlineButton {
	var rows [][]tg.InlineButton
	if u := tg.StartURL(mainUser, tg.AppWatchPayload(kind, tmdbID)); u != "" {
		rows = append(rows, []tg.InlineButton{{Text: playInAppBtn, URL: u, Style: tg.StylePrimary}})
	}
	if u := tg.StartURL(mainUser, tg.WatchLaterPayload(kind, tmdbID)); u != "" {
		rows = append(rows, []tg.InlineButton{{Text: laterBtn, URL: u}})
	}
	if tmdbID > 0 {
		rows = append(rows, []tg.InlineButton{shareButton(kind, tmdbID)})
	}
	rows = append(rows, []tg.InlineButton{
		{Text: searchBtn, SwitchInlineQueryCurrentChat: " "},
		{Text: homeBtn, Data: cbHome},
	})
	return rows
}

func shareButton(kind catalog.Kind, tmdbID uint64) tg.InlineButton {
	return tg.InlineButton{Text: shareBtn, SwitchInlineQuery: shareQuery(kind, tmdbID)}
}

func requestKeyboard(kind catalog.Kind, tmdbID uint64, mainUser string) tg.InlineKeyboard {
	rows := [][]tg.InlineButton{{
		{Text: requestBtn, Data: cbRequestPrefix + strconv.FormatUint(tmdbID, 10) + ":" + strconv.Itoa(int(kind))},
	}}
	rows = append(rows, titleActionRows(kind, tmdbID, mainUser)...)
	return tg.InlineKeyboard{Rows: rows}
}

func downloadKeyboard(kind catalog.Kind, tmdbID uint64, mainUser string) tg.InlineKeyboard {
	rows := [][]tg.InlineButton{{
		{
			Text:  downloadBtn,
			Data:  cbDownloadPrefix + strconv.FormatUint(tmdbID, 10) + ":" + strconv.Itoa(int(kind)),
			Style: tg.StyleSuccess,
		},
	}}
	rows = append(rows, titleActionRows(kind, tmdbID, mainUser)...)
	return tg.InlineKeyboard{Rows: rows}
}

func backRow(kind catalog.Kind, tmdbID uint64) []tg.InlineButton {
	return []tg.InlineButton{{
		Text: backBtn,
		Data: cbBackPrefix + strconv.FormatUint(tmdbID, 10) + ":" + strconv.Itoa(int(kind)),
	}}
}

func qualityKeyboard(files []catalog.File, tmdbID uint64, persian bool) tg.InlineKeyboard {
	rows := qualityRowsAt(files, persian)
	rows = append(rows, backRow(catalog.KindMovie, tmdbID))
	return tg.InlineKeyboard{Rows: rows}
}

func episodeQualityKeyboard(
	files []catalog.File, tmdbID uint64, season, ep int32, nums []int32, persian bool,
) tg.InlineKeyboard {
	rows := qualityRowsAt(files, persian)
	rows = append(rows, []tg.InlineButton{{
		Text:  allEpsBtn,
		Data:  cbBulkPrefix + strconv.FormatUint(tmdbID, 10) + ":" + strconv.Itoa(int(season)),
		Style: tg.StyleSuccess,
	}})
	if nav := episodeNavRow(tmdbID, season, ep, nums); len(nav) > 0 {
		rows = append(rows, nav)
	}
	rows = append(rows, seriesNavRows(tmdbID, season)...)
	return tg.InlineKeyboard{Rows: rows}
}

func bulkQualityKeyboard(tmdbID uint64, season int32, files []catalog.File, labels []string, persian bool) tg.InlineKeyboard {
	var rows [][]tg.InlineButton
	for i, label := range labels {
		text := label
		sample := sampleFileForLabel(files, label, persian)
		if sample.ID != 0 {
			text = fileButtonTextAt(sample, persian)
		}
		rows = append(rows, []tg.InlineButton{{
			Text:  text,
			Data:  fmt.Sprintf("%s%d:%d:%d", cbBulkDLPrefix, tmdbID, season, i),
			Style: fileButtonStyleAt(sample, persian),
		}})
	}
	rows = append(rows, seriesNavRows(tmdbID, season)...)
	return tg.InlineKeyboard{Rows: rows}
}

func qualityRows(files []catalog.File) [][]tg.InlineButton {
	return qualityRowsAt(files, false)
}

func qualityRowsAt(files []catalog.File, persian bool) [][]tg.InlineButton {
	ready := make([]catalog.File, 0, len(files))
	for _, f := range files {
		if f.State == catalog.FileReady {
			ready = append(ready, f)
		}
	}
	sort.SliceStable(ready, func(i, j int) bool {
		return filePickRankAt(ready[i], persian) < filePickRankAt(ready[j], persian)
	})
	best := make(map[string]catalog.File, len(ready))
	order := make([]string, 0, len(ready))
	for _, f := range ready {
		key := filePickKey(f, persian)
		prev, ok := best[key]
		if !ok {
			order = append(order, key)
			best[key] = f
			continue
		}
		if f.SizeBytes > prev.SizeBytes {
			best[key] = f
		}
	}
	rows := make([][]tg.InlineButton, 0, len(order))
	for _, key := range order {
		f := best[key]
		rows = append(rows, []tg.InlineButton{{
			Text:  fileButtonTextAt(f, persian),
			Data:  cbFilePrefix + strconv.FormatInt(f.ID, 10),
			Style: fileButtonStyleAt(f, persian),
		}})
	}
	return rows
}

// filePickRank orders the download list: plain, then soft/hard sub, then dub.
func filePickRank(f catalog.File) int {
	return filePickRankAt(f, false)
}

func filePickRankAt(f catalog.File, persian bool) int {
	spec := catalog.ParseFileSpec(f.QualityLabel)
	if fileIsDubbedAt(f, spec, persian) {
		return 2
	}
	if fileHasSubsAt(f, spec, persian) {
		return 1
	}
	return 0
}

// fileButtonStyle tints a quality button: default for a plain file, green for subtitles, blue for a dub.
func fileButtonStyle(f catalog.File) string {
	return fileButtonStyleAt(f, false)
}

func fileButtonStyleAt(f catalog.File, persian bool) string {
	spec := catalog.ParseFileSpec(f.QualityLabel)
	if fileIsDubbedAt(f, spec, persian) {
		return tg.StylePrimary
	}
	if fileHasSubsAt(f, spec, persian) {
		return tg.StyleSuccess
	}
	return ""
}

func episodeNavRow(tmdbID uint64, season, ep int32, nums []int32) []tg.InlineButton {
	prev, next, okPrev, okNext := adjacentEpisodes(ep, nums)
	var row []tg.InlineButton
	if okPrev {
		row = append(row, tg.InlineButton{
			Text: prevEpBtn,
			Data: fmt.Sprintf("%s%d:%d:%d", cbEpisodePrefix, tmdbID, season, prev),
		})
	}
	if okNext {
		row = append(row, tg.InlineButton{
			Text: nextEpBtn,
			Data: fmt.Sprintf("%s%d:%d:%d", cbEpisodePrefix, tmdbID, season, next),
		})
	}
	return row
}

func adjacentEpisodes(ep int32, nums []int32) (prev, next int32, okPrev, okNext bool) {
	for i, n := range nums {
		if n != ep {
			continue
		}
		if i > 0 {
			prev, okPrev = nums[i-1], true
		}
		if i+1 < len(nums) {
			next, okNext = nums[i+1], true
		}
		return prev, next, okPrev, okNext
	}
	return 0, 0, false, false
}

func seriesNavRows(tmdbID uint64, season int32) [][]tg.InlineButton {
	return [][]tg.InlineButton{
		{{
			Text: otherEpBtn,
			Data: cbSeasonPrefix + strconv.FormatUint(tmdbID, 10) + ":" + strconv.Itoa(int(season)),
		}},
		{{
			Text: backSeasons,
			Data: cbSeasonsPrefix + strconv.FormatUint(tmdbID, 10),
		}},
	}
}

func seasonKeyboard(tmdbID uint64, seasons []int32) tg.InlineKeyboard {
	var rows [][]tg.InlineButton
	for i := 0; i < len(seasons); i += 2 {
		row := []tg.InlineButton{seasonButton(tmdbID, seasons[i])}
		if i+1 < len(seasons) {
			row = append(row, seasonButton(tmdbID, seasons[i+1]))
		}
		rows = append(rows, row)
	}
	rows = append(rows, backRow(catalog.KindSeries, tmdbID))
	return tg.InlineKeyboard{Rows: rows}
}

func seasonButton(tmdbID uint64, season int32) tg.InlineButton {
	return tg.InlineButton{
		Text: fmt.Sprintf(seasonFmt, season),
		Data: cbSeasonPrefix + strconv.FormatUint(tmdbID, 10) + ":" + strconv.Itoa(int(season)),
	}
}

func episodeKeyboard(tmdbID uint64, season int32, nums []int32, page int) tg.InlineKeyboard {
	chunk, total, page := episodePageSlice(nums, page)
	var rows [][]tg.InlineButton
	for i := 0; i < len(chunk); i += 3 {
		var row []tg.InlineButton
		for j := i; j < i+3 && j < len(chunk); j++ {
			n := chunk[j]
			row = append(row, tg.InlineButton{
				Text: fmt.Sprintf(epFmt, n),
				Data: fmt.Sprintf("%s%d:%d:%d", cbEpisodePrefix, tmdbID, season, n),
			})
		}
		rows = append(rows, row)
	}
	if total > 1 {
		var nav []tg.InlineButton
		if page > 0 {
			nav = append(nav, tg.InlineButton{
				Text: "◀ قبلی",
				Data: fmt.Sprintf("%s%d:%d:%d", cbSeasonPrefix, tmdbID, season, page-1),
			})
		}
		nav = append(nav, tg.InlineButton{
			Text: fmt.Sprintf("%d / %d", page+1, total),
			Data: fmt.Sprintf("%s%d:%d:%d", cbSeasonPrefix, tmdbID, season, page),
		})
		if page+1 < total {
			nav = append(nav, tg.InlineButton{
				Text: "بعدی ▶",
				Data: fmt.Sprintf("%s%d:%d:%d", cbSeasonPrefix, tmdbID, season, page+1),
			})
		}
		rows = append(rows, nav)
	}
	rows = append(rows, []tg.InlineButton{{
		Text:  allEpsBtn,
		Data:  cbBulkPrefix + strconv.FormatUint(tmdbID, 10) + ":" + strconv.Itoa(int(season)),
		Style: tg.StyleSuccess,
	}})
	rows = append(rows, seriesNavRows(tmdbID, season)[1:]...)
	return tg.InlineKeyboard{Rows: rows}
}

func episodePageSlice(nums []int32, page int) (chunk []int32, total, safePage int) {
	total = (len(nums) + episodesPerPage - 1) / episodesPerPage
	if total < 1 {
		total = 1
	}
	if page < 0 {
		page = 0
	}
	if page >= total {
		page = total - 1
	}
	start := page * episodesPerPage
	if start >= len(nums) {
		return nil, total, page
	}
	end := start + episodesPerPage
	if end > len(nums) {
		end = len(nums)
	}
	return nums[start:end], total, page
}

func sampleFileForLabel(files []catalog.File, label string, persian bool) catalog.File {
	var best catalog.File
	for _, f := range files {
		if fileQualityLabelAt(f, persian) != label {
			continue
		}
		if best.ID == 0 || f.SizeBytes > best.SizeBytes {
			best = f
		}
	}
	return best
}

func distinctQualityLabels(filesByEp [][]catalog.File, persian bool) (labels []string, samples []catalog.File) {
	seen := map[string]struct{}{}
	for _, files := range filesByEp {
		for _, f := range files {
			if f.State != catalog.FileReady {
				continue
			}
			label := fileQualityLabelAt(f, persian)
			if _, ok := seen[label]; ok {
				continue
			}
			seen[label] = struct{}{}
			labels = append(labels, label)
			samples = append(samples, f)
		}
	}
	type pick struct {
		label string
		file  catalog.File
	}
	picks := make([]pick, len(labels))
	for i := range labels {
		picks[i] = pick{labels[i], samples[i]}
	}
	sort.SliceStable(picks, func(i, j int) bool {
		return filePickRankAt(picks[i].file, persian) < filePickRankAt(picks[j].file, persian)
	})
	for i := range picks {
		labels[i] = picks[i].label
		samples[i] = picks[i].file
	}
	return labels, samples
}
