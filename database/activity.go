package database

import (
	"fmt"
	"time"
)

// slotSeconds 是热度条的槽宽（5 分钟），flow_slots.slot_ts 与遗留 activity.slot_ts
// 都对齐到它的整数倍。
const slotSeconds int64 = 300

// FlowRecord 是采集器交给数据库层的一条上行流：五元组 + 绝对存活区间。
// 它只是传输结构，不落原始分片；SaveFlows 会按 (source, 五元组, 槽) 去重落库。
type FlowRecord struct {
	SrcIP    string
	SrcPort  uint16
	DstIP    string
	DstPort  uint16
	Protocol uint8
	Start    int64
	End      int64
}

// SaveFlows 把一个报文里的多条上行流按 (source, 五元组, 5 分钟槽) 去重写入
// flow_slots。主键保证同一连接在它覆盖的每个槽只留一行，因此长流被
// active-flow-timeout 切成多个分片、或分片重复上报，都不会重复计数。
// 每次调用只开一个事务，避免逐流提交的开销。
func (d *Database) SaveFlows(source string, flows []FlowRecord) error {
	if len(flows) == 0 {
		return nil
	}

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO flow_slots
		(source, src_ip, src_port, dst_ip, dst_port, proto, slot_ts)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, f := range flows {
		for ts := f.Start - f.Start%slotSeconds; ts <= f.End; ts += slotSeconds {
			if _, err = stmt.Exec(source, f.SrcIP, f.SrcPort, f.DstIP,
				f.DstPort, f.Protocol, ts); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// getFlowSlotGanttData 读取某个推送源某天的真实并发连接数，产出与 Clash 源同构的
// []GanttRow。flow_slots 每行是 (五元组, 槽) 唯一记录，按 (src_ip, 槽) 计数即
// 该客户端该槽的并发连接数，与 active-flow-timeout 切片长度无关。
func (d *Database) getFlowSlotGanttData(source, date string, normalize func(uint16) int) ([]GanttRow, error) {
	localLoc := time.Now().Location()
	dateStart, err := time.ParseInLocation("2006-01-02", date, localLoc)
	if err != nil {
		return nil, fmt.Errorf("解析日期失败: %v", err)
	}
	dateEnd := dateStart.Add(24 * time.Hour)

	rows, err := d.db.Query(`SELECT src_ip, slot_ts, COUNT(*) FROM flow_slots
		WHERE source = ? AND slot_ts >= ? AND slot_ts < ?
		GROUP BY src_ip, slot_ts`,
		source, dateStart.Unix(), dateEnd.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	raw := make(map[string]*[288]uint16)
	for rows.Next() {
		var ip string
		var ts int64
		var count int
		if err = rows.Scan(&ip, &ts, &count); err != nil {
			return nil, err
		}
		slot := calculateTimeSlotIndex(ts, dateStart)
		slots, ok := raw[ip]
		if !ok {
			slots = &[288]uint16{}
			raw[ip] = slots
		}
		// 单槽封顶，避免异常连接数把 uint16 撑爆
		if count > 65535 {
			count = 65535
		}
		slots[slot] = uint16(count)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return buildGanttRows(raw, normalize), nil
}

// CleanupFlowSlots 与 connections 共用同一个 cutoff 清理 flow_slots，避免新表无限增长。
func (d *Database) CleanupFlowSlots(cutoff int64) error {
	_, err := d.db.Exec("DELETE FROM flow_slots WHERE slot_ts < ?", cutoff)
	return err
}

// CleanupActivity 清理 v2.0.0 遗留的 activity 表数据。B 方案之后推送源不再写
// activity，但老库里可能还残留按「上行流数」聚合的旧数据（口径与并发连接数不同，
// 也没有五元组可迁移），让它随保留天数自然消失，不再 SELECT。
func (d *Database) CleanupActivity(cutoff int64) error {
	_, err := d.db.Exec("DELETE FROM activity WHERE slot_ts < ?", cutoff)
	return err
}
