package database

import (
	"fmt"
	"time"
)

// slotSeconds 是热度条的槽宽（5 分钟），activity.slot_ts 对齐到它的整数倍。
const slotSeconds int64 = 300

// SaveActivity 把一条上行流的活跃区间累加进 activity 表。
// 落库即聚合：不存原始流，只按 5 分钟槽累加活跃量，一条流在其存活区间覆盖的每个槽各 +1。
func (d *Database) SaveActivity(source, ip string, start, end int64) error {
	if end < start {
		start, end = end, start
	}

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO activity (source, ip, slot_ts, value) VALUES (?, ?, ?, 1)
		ON CONFLICT(source, ip, slot_ts) DO UPDATE SET value = value + 1`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for ts := start - start%slotSeconds; ts <= end; ts += slotSeconds {
		if _, err = stmt.Exec(source, ip, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// getActivityGanttData 读取某个推送源某天的活跃度，产出与 Clash 源同构的 []GanttRow。
func (d *Database) getActivityGanttData(source, date string, normalize func(uint16) int) ([]GanttRow, error) {
	localLoc := time.Now().Location()
	dateStart, err := time.ParseInLocation("2006-01-02", date, localLoc)
	if err != nil {
		return nil, fmt.Errorf("解析日期失败: %v", err)
	}
	dateEnd := dateStart.Add(24 * time.Hour)

	rows, err := d.db.Query(`SELECT ip, slot_ts, value FROM activity
		WHERE source = ? AND slot_ts >= ? AND slot_ts < ?`,
		source, dateStart.Unix(), dateEnd.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	raw := make(map[string]*[288]uint16)
	for rows.Next() {
		var ip string
		var ts int64
		var value int
		if err = rows.Scan(&ip, &ts, &value); err != nil {
			return nil, err
		}
		slot := calculateTimeSlotIndex(ts, dateStart)
		slots, ok := raw[ip]
		if !ok {
			slots = &[288]uint16{}
			raw[ip] = slots
		}
		// 单槽累加封顶，避免异常流把 uint16 撑爆
		if uint32(slots[slot])+uint32(value) < 65535 {
			slots[slot] += uint16(value)
		} else {
			slots[slot] = 65535
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return buildGanttRows(raw, normalize), nil
}

// CleanupActivity 与 connections 共用同一个 cutoff 清理 activity，避免新表无限增长。
func (d *Database) CleanupActivity(cutoff int64) error {
	_, err := d.db.Exec("DELETE FROM activity WHERE slot_ts < ?", cutoff)
	return err
}
