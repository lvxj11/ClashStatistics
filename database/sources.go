package database

import (
	"database/sql"
	"fmt"
)

// DataSource 是在线状态页的一个数据源定义。
// 凭据只存引用（环境变量名），不存明文；API 回显时也只有引用名。
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
	CredentialRef string `json:"credential_ref"` // 只存引用，不存值
	IsDefault     bool   `json:"is_default"`
	SortOrder     int    `json:"sort_order"`

	Thresholds       string `json:"thresholds"`        // 逗号分隔的 5 个档位分界值，空 = 用默认
	CustomThresholds bool   `json:"custom_thresholds"` // 是否启用自定义阈值
}

const sourceColumns = `id, type, name, enabled, address, port, lan_cidrs, exclude_ips,
	interval, credential_ref, is_default, sort_order, thresholds, custom_thresholds`

func scanSource(rows *sql.Rows) (DataSource, error) {
	var s DataSource
	var enabled, isDefault, customThresholds int
	err := rows.Scan(&s.ID, &s.Type, &s.Name, &enabled, &s.Address, &s.Port,
		&s.LANCIDRs, &s.ExcludeIPs, &s.Interval, &s.CredentialRef, &isDefault, &s.SortOrder,
		&s.Thresholds, &customThresholds)
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

// GetDefaultSource 返回默认数据源 ID；没有显式默认时回落到 clash。
func (d *Database) GetDefaultSource() (string, error) {
	var id string
	err := d.db.QueryRow(`SELECT id FROM data_sources WHERE is_default = 1 AND enabled = 1 LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return "clash", nil
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
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type, name = excluded.name, enabled = excluded.enabled,
			address = excluded.address, port = excluded.port, lan_cidrs = excluded.lan_cidrs,
			exclude_ips = excluded.exclude_ips, interval = excluded.interval,
			credential_ref = excluded.credential_ref, is_default = excluded.is_default,
			sort_order = excluded.sort_order,
			thresholds = excluded.thresholds, custom_thresholds = excluded.custom_thresholds`,
		s.ID, s.Type, s.Name, enabled, s.Address, s.Port, s.LANCIDRs, s.ExcludeIPs,
		s.Interval, s.CredentialRef, isDefault, s.SortOrder, s.Thresholds, custom)
	return err
}

// DeleteSource 删除一个数据源。内置 clash 源不允许删除，避免升级后失去默认视图。
func (d *Database) DeleteSource(id string) error {
	if id == "clash" {
		return fmt.Errorf("内置 Clash 源不能删除，只能停用")
	}
	_, err := d.db.Exec(`DELETE FROM data_sources WHERE id = ?`, id)
	return err
}

// BootstrapConfig 是首次启动（表为空）时写入默认数据源所需的配置值。
type BootstrapConfig struct {
	ClashHost     string
	ClashInterval int
	IPFIXPort     int
	IPFIXLAN      string
	IPFIXExclude  string
}

// EnsureDefaultSources 在 data_sources 为空时写入两条内置源，保证从 v1.0.4
// 升级上来零配置可用。仅在表为空时执行，之后完全以表为准，不覆盖用户改动。
func (d *Database) EnsureDefaultSources(b BootstrapConfig) error {
	var count int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM data_sources`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	clash := DataSource{
		ID: "clash", Type: "clash", Name: "Clash 代理", Enabled: true,
		Address: b.ClashHost, Interval: b.ClashInterval,
		CredentialRef: "CLASH_SECRET", IsDefault: true, SortOrder: 0,
	}
	ipfix := DataSource{
		ID: "ipfix", Type: "ipfix", Name: "路由器流量 (IPFIX)", Enabled: true,
		Address: "0.0.0.0", Port: b.IPFIXPort,
		LANCIDRs: b.IPFIXLAN, ExcludeIPs: b.IPFIXExclude, SortOrder: 1,
	}
	if err := d.UpsertSource(clash); err != nil {
		return err
	}
	return d.UpsertSource(ipfix)
}

// migrateSourceThresholds 为已存在的 data_sources 表补齐自定义阈值列。
// CREATE TABLE IF NOT EXISTS 不会改动旧表结构，所以用 PRAGMA 判列后幂等 ALTER。
// 仅当本次真正补了列（从旧版升级）时，把升级前硬编码的 IPFIX 阈值迁到内置
// ipfix 行，保证升级零回退；此后用户在管理页怎么改都不会被覆盖。
func migrateSourceThresholds(db *sql.DB) error {
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

	added := false
	if !existing["thresholds"] {
		if _, err := db.Exec(`ALTER TABLE data_sources ADD COLUMN thresholds TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
		added = true
	}
	if !existing["custom_thresholds"] {
		if _, err := db.Exec(`ALTER TABLE data_sources ADD COLUMN custom_thresholds INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !added {
		return nil
	}
	_, err = db.Exec(`UPDATE data_sources SET thresholds = '2,50,200,600,900', custom_thresholds = 1
		WHERE id = 'ipfix' AND thresholds = ''`)
	return err
}
