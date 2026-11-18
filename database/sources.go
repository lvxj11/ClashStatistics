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
	Enabled       bool   `json:"enabled"`        // 采集开关：只控制是否起采集器写库，与能否被选中无关
	Address       string `json:"address"`        // clash: host:port；ipfix: 监听地址
	Port          int    `json:"port"`           // ipfix: UDP 端口
	LANCIDRs      string `json:"lan_cidrs"`      // ipfix: 逗号分隔的 LAN 网段
	ExcludeIPs    string `json:"exclude_ips"`    // ipfix: 逗号分隔的排除 IP
	Interval      int    `json:"interval"`       // clash: 采集间隔（毫秒）
	CredentialRef string `json:"credential_ref"` // 凭据引用（环境变量名），非空时优先
	// 表内保存的轻量混淆后密钥；json:"-" 保证永不回显，只写字段由 API 层收明文后混淆。
	CredentialValue string `json:"-"`
	IsDefault       bool   `json:"is_default"` // 在线默认（单选），只决定在线状态页跟随哪个源
	SortOrder       int    `json:"sort_order"`

	// InSummary 只控制该 clash 源是否参与目标统计 / 客户端统计（含明细弹窗）。
	// ipfix 源不写 connections，汇总开关恒为 false（API 层强制、查询层也只认 clash）。
	InSummary        bool   `json:"in_summary"`
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
	interval, credential_ref, credential_value, is_default, sort_order, thresholds, custom_thresholds, in_summary`

func scanSource(rows *sql.Rows) (DataSource, error) {
	var s DataSource
	var enabled, isDefault, customThresholds, inSummary int
	err := rows.Scan(&s.ID, &s.Type, &s.Name, &enabled, &s.Address, &s.Port,
		&s.LANCIDRs, &s.ExcludeIPs, &s.Interval, &s.CredentialRef, &s.CredentialValue,
		&isDefault, &s.SortOrder, &s.Thresholds, &customThresholds, &inSummary)
	s.Enabled = enabled != 0
	s.IsDefault = isDefault != 0
	s.CustomThresholds = customThresholds != 0
	s.InSummary = inSummary != 0
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

// GetEnabledSources 返回启用的数据源，供采集器使用（enabled 只控制采集落盘）。
func (d *Database) GetEnabledSources() ([]DataSource, error) {
	return collectSources(d.db.Query(`SELECT ` + sourceColumns + ` FROM data_sources WHERE enabled = 1 ORDER BY sort_order, id`))
}

// GetDefaultSource 返回「在线默认」数据源 ID。显式默认优先，且不受采集开关影响
// （采集关闭的源仍可被选为在线默认、可看历史数据）；没有显式默认时回落到第一个
// 启用源；一个都没有（空表）时返回空串。v3.0.0 起不再硬编码 "clash"。
func (d *Database) GetDefaultSource() (string, error) {
	var id string
	err := d.db.QueryRow(`SELECT id FROM data_sources WHERE is_default = 1 LIMIT 1`).Scan(&id)
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

// GetSourceType 返回数据源类型（clash / ipfix）；源不存在时返回空串。
// 供在线状态查询按真实数据源类型路由（clash → connections，ipfix → flow_slots）。
func (d *Database) GetSourceType(id string) (string, error) {
	var typ string
	err := d.db.QueryRow(`SELECT type FROM data_sources WHERE id = ?`, id).Scan(&typ)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return typ, err
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
	enabled, isDefault, custom, inSummary := 0, 0, 0, 0
	if s.Enabled {
		enabled = 1
	}
	if s.IsDefault {
		isDefault = 1
	}
	if s.CustomThresholds {
		custom = 1
	}
	// ipfix 源不写 connections，汇总开关强制为 0（管理页禁用该开关，这里是兜底）。
	if s.InSummary && s.Type != "ipfix" {
		inSummary = 1
	}
	_, err := d.db.Exec(`INSERT INTO data_sources (`+sourceColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type, name = excluded.name, enabled = excluded.enabled,
			address = excluded.address, port = excluded.port, lan_cidrs = excluded.lan_cidrs,
			exclude_ips = excluded.exclude_ips, interval = excluded.interval,
			credential_ref = excluded.credential_ref, credential_value = excluded.credential_value,
			is_default = excluded.is_default, sort_order = excluded.sort_order,
			thresholds = excluded.thresholds, custom_thresholds = excluded.custom_thresholds,
			in_summary = excluded.in_summary`,
		s.ID, s.Type, s.Name, enabled, s.Address, s.Port, s.LANCIDRs, s.ExcludeIPs,
		s.Interval, s.CredentialRef, s.CredentialValue, isDefault, s.SortOrder, s.Thresholds, custom, inSummary)
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
