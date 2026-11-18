package database

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// DefaultThresholds 是各数据源未开启自定义阈值时共用的 1-6 档分界值。
// 沿用改造前前端 heatColor 的阈值，保证单 Clash 源逐槽颜色零回归。
var DefaultThresholds = []uint16{2, 20, 50, 150, 254}

// ParseThresholds 校验并解析逗号分隔的 5 个分界值：必须正好 5 个、正整数、
// 严格递增且不超过 uint16 上限。供 API 保存前校验，也供查询时兜底解析。
func ParseThresholds(s string) ([]uint16, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 5 {
		return nil, fmt.Errorf("阈值必须是 5 个逗号分隔的数值")
	}
	out := make([]uint16, 0, 5)
	var prev int64
	for _, p := range parts {
		n, err := strconv.ParseInt(strings.TrimSpace(p), 10, 32)
		if err != nil || n <= 0 || n > 65535 {
			return nil, fmt.Errorf("阈值必须是 1-65535 的整数: %q", p)
		}
		if n <= prev {
			return nil, fmt.Errorf("阈值必须严格递增")
		}
		prev = n
		out = append(out, uint16(n))
	}
	return out, nil
}

// toIntensity 把原始槽值映射到 1-6 档，0 表示无活动。
func toIntensity(v uint16, thresholds []uint16) int {
	if v == 0 {
		return 0
	}
	for i, t := range thresholds {
		if v <= t {
			return i + 1
		}
	}
	return 6
}

// sourceThresholds 返回某数据源生效的阈值。未开启自定义、留空、源不存在或
// 值损坏时一律回落到 DefaultThresholds，保证任何时候都能出图。
// v3.0.0 起不再有硬编码 "clash" 源：source 就是 data_sources 里的真实 ID。
func (d *Database) sourceThresholds(source string) []uint16 {
	var custom int
	var raw string
	if err := d.db.QueryRow(`SELECT custom_thresholds, thresholds FROM data_sources WHERE id = ?`, source).
		Scan(&custom, &raw); err != nil || custom == 0 || raw == "" {
		return DefaultThresholds
	}
	t, err := ParseThresholds(raw)
	if err != nil {
		return DefaultThresholds
	}
	return t
}

// buildGanttRows 是各源共用的稀疏化出口：原始槽值 → []GanttRow。
// 各源只负责产出 map[ip]*[288]uint16，归一化函数由调用方按数据源阈值构造，
// 这里不按数据源类型分支，也不产生任何按类型判断的内容。
func buildGanttRows(raw map[string]*[288]uint16, normalize func(uint16) int) []GanttRow {
	result := make([]GanttRow, 0, len(raw))
	for ip, slots := range raw {
		row := GanttRow{
			SourceIP:    ip,
			SlotIndexes: []int{},
			SlotValues:  []int{},
			RawValues:   []int{},
		}
		for slot, v := range slots {
			if level := normalize(v); level > 0 {
				row.SlotIndexes = append(row.SlotIndexes, slot)
				row.SlotValues = append(row.SlotValues, level)
				row.RawValues = append(row.RawValues, int(v))
			}
		}
		if len(row.SlotIndexes) > 0 {
			result = append(result, row)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return naturalIPCompare(result[i].SourceIP, result[j].SourceIP)
	})
	return result
}

// GetGanttData 是在线状态页的统一查询出口：各源都返回同一结构的 []GanttRow。
// v3.0.0 起严格按「在线默认」单选的真实数据源 ID 取数，不再有下拉切换、也不再
// 硬编码 "clash"：clash 源读 connections WHERE source=该ID，ipfix 源读对应的
// flow_slots。未知 / 已删除 / 空源一律返回空结果，避免把不同源的数据混在一起。
func (d *Database) GetGanttData(source, date string) ([]GanttRow, error) {
	if source == "" {
		return []GanttRow{}, nil
	}
	typ, err := d.GetSourceType(source)
	if err != nil {
		return nil, err
	}
	thresholds := d.sourceThresholds(source)
	normalize := func(v uint16) int { return toIntensity(v, thresholds) }
	switch typ {
	case "clash":
		return d.getClashGanttData(source, date, normalize)
	case "ipfix":
		return d.getFlowSlotGanttData(source, date, normalize)
	default:
		return []GanttRow{}, nil
	}
}
