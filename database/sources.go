package database

import (
	"database/sql"
	"fmt"
)

// DataSource 是在线状态页的一个数据源定义，也是运行时唯一的数据源配置来源。
// 凭据有两种来源（见 CredentialSource）：环境变量引用，或表内轻量混淆后直接保存；
// CredentialValue 永不进 JSON（API / 页面 / 日志都不回显混淆串或明文）。
type DataSource struct {
	ID            string `json:"id"`
	Type          string `json:"type"` // clash | ipfix
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
	Address       string `json:"address"`        // clash: host:port；ipfix: 监听地址
	Port          int    `json:"port"`           // ipfix: UDP 端口
	LANCIDRs      string `json:"lan_cidrs"`      // ipfix: 逗号分隔的 LAN 网段
	ExcludeIPs    string `json:"exclude_ips"`    // ipfix: 逗号分隔的排除 IP
	Interval      int    `json:"interval"`       // clash: 采集间隔（毫秒）
	CredentialRef string `json:"credential_ref"` // 凭据引用（环境变量名），非空时优先
	// 表内保存的轻量混淆后密钥；json:"-" 保证永不回显，只写字段由 API 层收明文后混淆。
	CredentialValue string `json:"-"`
	IsDefault       bool   `json:"is_default"`
	SortOrder       int    `json:"sort_order"`

	Thresholds       string `json:"thresholds"`        // 逗号分隔的 5 个档位分界值，空 = 用默认
	CustomThresholds bool   `json:"custom_thresholds"` // 是否启用自定义阈值
}

// CredentialSource 返回凭据来源："env"（引用环境变量）/ "table"（表内混淆值）/ ""（未配置）。
func (s DataSource) CredentialSource() string {
	if s.CredentialRef != "" {
		return "env"
	}
	if s.CredentialValue != "" {
		return "table"
	}
	return ""
}

const sourceColumns = `id, type, name, enabled, address, port, lan_cidrs, exclude_ips,
	interval, credential_ref, credential_value, is_default, sort_order, thresholds, custom_thresholds`

func scanSource(rows *sql.Rows) (DataSource, error) {
	var s DataSource
	var enabled, isDefault, customThresholds int
	err := rows.Scan(&s.ID, &s.Type, &s.Name, &enabled, &s.Address, &s.Port,
		&s.LANCIDRs, &s.ExcludeIPs, &s.Interval, &s.CredentialRef, &s.CredentialValue,
		&isDefault, &s.SortOrder, &s.Thresholds, &customThresholds)
	s.Enabled = enabled != 0
	s.IsDefault = isDefault != 0
	s.CustomThresholds = customThresholds != 0
	return s, err
}

func collectSources(rows *sql.Rows, err error) ([]DataSource, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sources := []DataSource{}
	for rows.Next() {
		s, scanErr := scanSource(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		sources = append(sources, s)
	}
	return sources, rows.Err()
}

// GetSources 返回全部数据源（含禁用），供管理页使用。
func (d *Database) GetSources() ([]DataSource, error) {
	return collectSources(d.db.Query(`SELECT ` + sourceColumns + ` FROM data_sources ORDER BY sort_order, id`))
}

// GetEnabledSources 返回启用的数据源，供采集器与前端下拉使用。
func (d *Database) GetEnabledSources() ([]DataSource, error) {
	return collectSources(d.db.Query(`SELECT ` + sourceColumns + ` FROM data_sources WHERE enabled = 1 ORDER BY sort_order, id`))
}

// GetDefaultSource 返回默认数据源 ID。没有显式默认时回落到第一个启用源；
// 一个都没有（空表）时返回空串，由前端提示「暂无数据源」。不再硬编码 "clash"。
func (d *Database) GetDefaultSource() (string, error) {
	var id string
	err := d.db.QueryRow(`SELECT id FROM data_sources WHERE is_default = 1 AND enabled = 1 LIMIT 1`).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	err = d.db.QueryRow(`SELECT id FROM data_sources WHERE enabled = 1 ORDER BY sort_order, id LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// UpsertSource 新增或更新一个数据源。设为默认时先清掉其它源的默认标记。
func (d *Database) UpsertSource(s DataSource) error {
	if s.ID == "" {
		return fmt.Errorf("数据源 ID 不能为空")
	}
	if s.Type != "clash" && s.Type != "ipfix" {
		return fmt.Errorf("不支持的数据源类型: %s", s.Type)
	}
	if s.IsDefault {
		if _, err := d.db.Exec(`UPDATE data_sources SET is_default = 0`); err != nil {
			return err
		}
	}
	enabled, isDefault, custom := 0, 0, 0
	if s.Enabled {
		enabled = 1
	}
	if s.IsDefault {
		isDefault = 1
	}
	if s.CustomThresholds {
		custom = 1
	}
	_, err := d.db.Exec(`INSERT INTO data_sources (`+sourceColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type, name = excluded.name, enabled = excluded.enabled,
			address = excluded.address, port = excluded.port, lan_cidrs = excluded.lan_cidrs,
			exclude_ips = excluded.exclude_ips, interval = excluded.interval,
			credential_ref = excluded.credential_ref, credential_value = excluded.credential_value,
			is_default = excluded.is_default, sort_order = excluded.sort_order,
			thresholds = excluded.thresholds, custom_thresholds = excluded.custom_thresholds`,
		s.ID, s.Type, s.Name, enabled, s.Address, s.Port, s.LANCIDRs, s.ExcludeIPs,
		s.Interval, s.CredentialRef, s.CredentialValue, isDefault, s.SortOrder, s.Thresholds, custom)
	return err
}

// GetSourceCredentialValue 读取某个数据源表内保存的混淆凭据（供编辑时「留空不修改」保留旧值）。
// 数据源不存在时返回空串，不是错误。
func (d *Database) GetSourceCredentialValue(id string) (string, error) {
	var value string
	err := d.db.QueryRow(`SELECT credential_value FROM data_sources WHERE id = ?`, id).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// DeleteSource 删除一个数据源。空表是合法状态（不做任何播种），删光后重启仍保持空表，
// 所以不禁止删除任何数据源（包括 clash 源）。
func (d *Database) DeleteSource(id string) error {
	_, err := d.db.Exec(`DELETE FROM data_sources WHERE id = ?`, id)
	return err
}

// migrateSourceColumns 为已存在的 data_sources 表补齐后加的列（自定义阈值、
// 表内混淆凭据）。CREATE TABLE IF NOT EXISTS 不会改动旧表结构，所以用 PRAGMA
// 判列后幂等 ALTER。仅当本次真正补了 thresholds 列（从旧版升级）时，把升级前
// 硬编码的 IPFIX 阈值迁到内置 ipfix 行，保证升级零回退；此后管理页改动不会被覆盖。
func migrateSourceColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(data_sources)`)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	addedThresholds := false
	if !existing["thresholds"] {
		if _, err := db.Exec(`ALTER TABLE data_sources ADD COLUMN thresholds TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
		addedThresholds = true
	}
	if !existing["custom_thresholds"] {
		if _, err := db.Exec(`ALTER TABLE data_sources ADD COLUMN custom_thresholds INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !existing["credential_value"] {
		if _, err := db.Exec(`ALTER TABLE data_sources ADD COLUMN credential_value TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !addedThresholds {
		return nil
	}
	_, err = db.Exec(`UPDATE data_sources SET thresholds = '2,50,200,600,900', custom_thresholds = 1
		WHERE id = 'ipfix' AND thresholds = ''`)
	return err
}
