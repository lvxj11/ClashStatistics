package database

import (
	"fmt"
	"sort"
	"time"
)

// ClientName 是某个客户端 IP 的名称映射。
// 手动与自动字段分开保存：UpsertAutoName 只写 auto_* 列，UpsertManualName 只写
// manual_* 列，两者互不覆盖。Name/Source 是"生效值"，由 effectiveName 计算，
// 不落库，避免两处真值不一致。
type ClientName struct {
	IP            string `json:"ip"`
	ManualName    string `json:"manual_name"`
	Note          string `json:"note"`
	AutoName      string `json:"auto_name"`
	AutoSource    string `json:"auto_source"`
	AutoUpdated   int64  `json:"auto_updated"`
	ManualUpdated int64  `json:"manual_updated"`
	Name          string `json:"name"`   // 生效名称：手动优先，否则自动
	Source        string `json:"source"` // manual / auto_source / ""
}

// effectiveName 计算生效名称与来源。手动配置非空时忽略一切自动值。
func effectiveName(rec *ClientName) (string, string) {
	if rec.ManualName != "" {
		return rec.ManualName, "manual"
	}
	if rec.AutoName != "" {
		return rec.AutoName, rec.AutoSource
	}
	return "", ""
}

// GetClientNames 返回所有"有内容"的名称记录（含只有自动名称的），供管理页与展示接口使用。
func (d *Database) GetClientNames() ([]ClientName, error) {
	rows, err := d.db.Query(`SELECT ip, manual_name, note, auto_name, auto_source, auto_updated, manual_updated
		FROM client_names
		WHERE manual_name != '' OR auto_name != '' OR note != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []ClientName{}
	for rows.Next() {
		var rec ClientName
		if err := rows.Scan(&rec.IP, &rec.ManualName, &rec.Note, &rec.AutoName,
			&rec.AutoSource, &rec.AutoUpdated, &rec.ManualUpdated); err != nil {
			return nil, err
		}
		rec.Name, rec.Source = effectiveName(&rec)
		result = append(result, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// GetKnownSourceIPs 列出统计里出现过的客户端键，供管理页"给已知 IP 命名"。
// 复用聚合用的 sourceIPExpr，保证这里的键与统计页显示的完全一致（含 Inner 等）。
func (d *Database) GetKnownSourceIPs() ([]string, error) {
	query := fmt.Sprintf(`SELECT DISTINCT %s AS ip FROM connections
		WHERE (source_ip IS NOT NULL AND source_ip != '') OR type = 'Inner'`, sourceIPExpr)
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ips := []string{}
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		if ip != "" {
			ips = append(ips, ip)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(ips, func(i, j int) bool { return naturalIPCompare(ips[i], ips[j]) })
	return ips, nil
}

// UpsertManualName 写入手动名称与备注。只碰 manual_* / note 列，自动列保持原样。
func (d *Database) UpsertManualName(ip, name, note string) error {
	now := time.Now().Unix()
	_, err := d.db.Exec(`INSERT INTO client_names (ip, manual_name, note, manual_updated)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(ip) DO UPDATE SET
			manual_name = excluded.manual_name,
			note = excluded.note,
			manual_updated = excluded.manual_updated`,
		ip, name, note, now)
	return err
}

// ClearManualName 清除手动配置（保留自动名称作为兜底）。
// 若该行已无任何内容，则删掉，避免表里堆积空行。
func (d *Database) ClearManualName(ip string) error {
	if _, err := d.db.Exec(`UPDATE client_names
		SET manual_name = '', note = '', manual_updated = 0 WHERE ip = ?`, ip); err != nil {
		return err
	}
	_, err := d.db.Exec(`DELETE FROM client_names
		WHERE ip = ? AND manual_name = '' AND note = '' AND auto_name = ''`, ip)
	return err
}

// UpsertAutoName 写入自动识别的名称。只碰 auto_* 列，绝不触碰手动配置。
// 由自动发现流程调用；优先级判断在发现层完成，这里只负责落地。
func (d *Database) UpsertAutoName(ip, name, source string, updated int64) error {
	if updated == 0 {
		updated = time.Now().Unix()
	}
	_, err := d.db.Exec(`INSERT INTO client_names (ip, auto_name, auto_source, auto_updated)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(ip) DO UPDATE SET
			auto_name = excluded.auto_name,
			auto_source = excluded.auto_source,
			auto_updated = excluded.auto_updated`,
		ip, name, source, updated)
	return err
}
