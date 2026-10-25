package config

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"eflow.local/new-api/common"
)

// ConfigManager 统一管理所有配置
type ConfigManager struct {
	configs map[string]any
	mutex   sync.RWMutex
}

var GlobalConfig = NewConfigManager()

func NewConfigManager() *ConfigManager {
	return &ConfigManager{
		configs: make(map[string]any),
	}
}

// Register registers an owned copy so the caller cannot mutate published state
// without going through the manager.
func (cm *ConfigManager) Register(name string, config any) {
	owned, err := cloneConfig(config)
	if err != nil {
		panic(fmt.Sprintf("register config %q: %v", name, err))
	}
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.configs[name] = owned
}

func (cm *ConfigManager) Has(name string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	_, ok := cm.configs[name]
	return ok
}

// IsBooleanField reports whether key identifies a bool field in a registered
// config module. It follows the same direct JSON-field mapping used by updates.
func (cm *ConfigManager) IsBooleanField(name, key string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	cfg, ok := cm.configs[name]
	if !ok {
		return false
	}
	typ := reflect.TypeOf(cfg)
	if typ.Kind() != reflect.Ptr || typ.Elem().Kind() != reflect.Struct {
		return false
	}
	typ = typ.Elem()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
		if jsonName == "" {
			jsonName = field.Name
		}
		if jsonName == key {
			return field.Type.Kind() == reflect.Bool
		}
	}
	return false
}

// Get returns an isolated copy of a configuration module.
func (cm *ConfigManager) Get(name string) any {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	cfg, ok := cm.configs[name]
	if !ok {
		return nil
	}
	clone, err := cloneConfig(cfg)
	if err != nil {
		return nil
	}
	return clone
}

// Update parses and publishes a module update while holding the manager lock.
func (cm *ConfigManager) Update(name string, values map[string]string) (bool, error) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cfg, ok := cm.configs[name]
	if !ok {
		return false, nil
	}
	return true, updateConfigFromMap(cfg, values)
}

// Validate checks whether a module update can be parsed without publishing it.
func (cm *ConfigManager) Validate(name string, values map[string]string) (bool, error) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	cfg, ok := cm.configs[name]
	if !ok {
		return false, nil
	}
	clone, err := cloneConfig(cfg)
	if err != nil {
		return true, err
	}
	return true, updateConfigFromMap(clone, values)
}

// Snapshot copies a registered module into destination while updates are
// excluded. destination must point to the module's concrete struct type.
func (cm *ConfigManager) Snapshot(name string, destination any) (bool, error) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	cfg, ok := cm.configs[name]
	if !ok {
		return false, nil
	}
	return true, cloneConfigInto(cfg, destination)
}

// MustSnapshot returns an isolated snapshot from the global manager. A missing
// registration or an incompatible type is a programming error.
func MustSnapshot[T any](name string) T {
	var snapshot T
	ok, err := GlobalConfig.Snapshot(name, &snapshot)
	if err != nil {
		panic(fmt.Sprintf("snapshot config %q: %v", name, err))
	}
	if !ok {
		panic(fmt.Sprintf("snapshot config %q: not registered", name))
	}
	return snapshot
}

// Read executes fn with an isolated copy of the registered config.
func (cm *ConfigManager) Read(name string, fn func(any)) bool {
	cm.mutex.RLock()
	cfg, ok := cm.configs[name]
	if !ok {
		cm.mutex.RUnlock()
		return false
	}
	clone, err := cloneConfig(cfg)
	cm.mutex.RUnlock()
	if err != nil {
		return false
	}
	if fn != nil {
		fn(clone)
	}
	return true
}

// LoadFromDB 从数据库加载配置
func (cm *ConfigManager) LoadFromDB(options map[string]string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	candidates := make(map[string]any)
	for name, current := range cm.configs {
		prefix := name + "."
		configMap := make(map[string]string)

		// 收集属于此配置的所有选项
		for key, value := range options {
			if after, ok := strings.CutPrefix(key, prefix); ok {
				configKey := after
				configMap[configKey] = value
			}
		}

		if len(configMap) > 0 {
			candidate, err := cloneConfig(current)
			if err != nil {
				return fmt.Errorf("failed to clone config %s: %w", name, err)
			}
			if err = updateConfigFromMap(candidate, configMap); err != nil {
				return fmt.Errorf("failed to update config %s: %w", name, err)
			}
			candidates[name] = candidate
		}
	}

	for name, candidate := range candidates {
		if err := replaceConfig(cm.configs[name], candidate); err != nil {
			return fmt.Errorf("failed to publish config %s: %w", name, err)
		}
	}

	return nil
}

// SaveToDB 将配置保存到数据库
func (cm *ConfigManager) SaveToDB(updateFunc func(key, value string) error) error {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	for name, config := range cm.configs {
		configMap, err := configToMap(config)
		if err != nil {
			return err
		}

		for key, value := range configMap {
			dbKey := name + "." + key
			if err := updateFunc(dbKey, value); err != nil {
				return err
			}
		}
	}

	return nil
}

// 辅助函数：将配置对象转换为map
func configToMap(config any) (map[string]string, error) {
	result := make(map[string]string)

	val := reflect.ValueOf(config)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, nil
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// 跳过未导出字段
		if !fieldType.IsExported() {
			continue
		}

		// 获取json标签作为键名
		key := fieldType.Tag.Get("json")
		if key == "" || key == "-" {
			key = fieldType.Name
		}

		// 处理不同类型的字段
		var strValue string
		switch field.Kind() {
		case reflect.String:
			strValue = field.String()
		case reflect.Bool:
			strValue = strconv.FormatBool(field.Bool())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			strValue = strconv.FormatInt(field.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			strValue = strconv.FormatUint(field.Uint(), 10)
		case reflect.Float32, reflect.Float64:
			strValue = strconv.FormatFloat(field.Float(), 'f', -1, 64)
		case reflect.Ptr:
			// 处理指针类型：如果非 nil，序列化指向的值
			if !field.IsNil() {
				bytes, err := common.Marshal(field.Interface())
				if err != nil {
					return nil, err
				}
				strValue = string(bytes)
			} else {
				// nil 指针序列化为 "null"
				strValue = "null"
			}
		case reflect.Map, reflect.Slice, reflect.Struct:
			// 复杂类型使用JSON序列化
			bytes, err := common.Marshal(field.Interface())
			if err != nil {
				return nil, err
			}
			strValue = string(bytes)
		default:
			// 跳过不支持的类型
			continue
		}

		result[key] = strValue
	}

	return result, nil
}

func cloneConfig(config any) (any, error) {
	val := reflect.ValueOf(config)
	if val.Kind() != reflect.Ptr || val.IsNil() || val.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("config must point to a struct, got %T", config)
	}
	clone := reflect.New(val.Elem().Type()).Interface()
	serialized, err := common.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	if err = common.Unmarshal(serialized, clone); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return clone, nil
}

func cloneConfigInto(config any, destination any) error {
	clone, err := cloneConfig(config)
	if err != nil {
		return err
	}
	return replaceConfig(destination, clone)
}

func replaceConfig(destination any, source any) error {
	dst := reflect.ValueOf(destination)
	src := reflect.ValueOf(source)
	if dst.Kind() != reflect.Ptr || dst.IsNil() || dst.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("destination must point to a struct, got %T", destination)
	}
	if src.Kind() != reflect.Ptr || src.IsNil() || src.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("source must point to a struct, got %T", source)
	}
	if dst.Elem().Type() != src.Elem().Type() {
		return fmt.Errorf("config type mismatch: destination %T, source %T", destination, source)
	}
	dst.Elem().Set(src.Elem())
	return nil
}

// 辅助函数：从map更新配置对象
func updateConfigFromMap(config any, configMap map[string]string) error {
	val := reflect.ValueOf(config)
	if val.Kind() != reflect.Ptr {
		return fmt.Errorf("config must be a pointer, got %T", config)
	}
	if val.IsNil() || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("config must point to a struct, got %T", config)
	}
	original := val.Elem()
	clone := reflect.New(original.Type())
	serialized, err := common.Marshal(config)
	if err != nil {
		return fmt.Errorf("clone config: %w", err)
	}
	if err = common.Unmarshal(serialized, clone.Interface()); err != nil {
		return fmt.Errorf("clone config: %w", err)
	}
	if err = applyConfigFromMap(clone.Elem(), configMap); err != nil {
		return err
	}
	original.Set(clone.Elem())
	return nil
}

func applyConfigFromMap(val reflect.Value, configMap map[string]string) error {
	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// 跳过未导出字段
		if !fieldType.IsExported() {
			continue
		}

		// 获取json标签作为键名
		key := fieldType.Tag.Get("json")
		if key == "" || key == "-" {
			key = fieldType.Name
		}

		// 检查map中是否有对应的值
		strValue, ok := configMap[key]
		if !ok {
			continue
		}

		// 根据字段类型设置值
		if !field.CanSet() {
			continue
		}

		switch field.Kind() {
		case reflect.String:
			field.SetString(strValue)
		case reflect.Bool:
			boolValue, err := strconv.ParseBool(strValue)
			if err != nil {
				return fmt.Errorf("parse %s as bool: %w", key, err)
			}
			field.SetBool(boolValue)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			intValue, err := parseCompatibleInt(strValue, field.Type().Bits())
			if err != nil {
				return fmt.Errorf("parse %s as int: %w", key, err)
			}
			field.SetInt(intValue)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			uintValue, err := parseCompatibleUint(strValue, field.Type().Bits())
			if err != nil {
				return fmt.Errorf("parse %s as uint: %w", key, err)
			}
			field.SetUint(uintValue)
		case reflect.Float32, reflect.Float64:
			floatValue, err := strconv.ParseFloat(strValue, 64)
			if err != nil {
				return fmt.Errorf("parse %s as float: %w", key, err)
			}
			if math.IsNaN(floatValue) || math.IsInf(floatValue, 0) {
				return fmt.Errorf("parse %s as float: value must be finite", key)
			}
			field.SetFloat(floatValue)
		case reflect.Ptr:
			// 处理指针类型
			if strValue == "null" {
				field.Set(reflect.Zero(field.Type()))
			} else {
				// 如果指针是 nil，需要先初始化
				if field.IsNil() {
					field.Set(reflect.New(field.Type().Elem()))
				}
				// 反序列化到指针指向的值
				err := common.Unmarshal([]byte(strValue), field.Interface())
				if err != nil {
					return fmt.Errorf("parse %s: %w", key, err)
				}
			}
		case reflect.Map:
			// json.Unmarshal merges into existing maps (keeps old keys that are
			// absent from the new JSON). Allocate a fresh map so removed keys
			// are properly cleared.
			fresh := reflect.New(field.Type())
			if err := common.Unmarshal([]byte(strValue), fresh.Interface()); err != nil {
				return fmt.Errorf("parse %s: %w", key, err)
			}
			field.Set(fresh.Elem())
		case reflect.Slice:
			// Decoding into an existing slice reuses its elements, so fields the
			// stored JSON omits would inherit values from the element that used
			// to sit at the same index (and element maps would be merged).
			fresh := reflect.New(field.Type())
			if err := common.Unmarshal([]byte(strValue), fresh.Interface()); err != nil {
				return fmt.Errorf("parse %s: %w", key, err)
			}
			field.Set(fresh.Elem())
		case reflect.Struct:
			err := common.Unmarshal([]byte(strValue), field.Addr().Interface())
			if err != nil {
				return fmt.Errorf("parse %s: %w", key, err)
			}
		}
	}

	return nil
}

func parseCompatibleInt(value string, bitSize int) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, bitSize)
	if err == nil {
		return parsed, nil
	}
	floatValue, floatErr := strconv.ParseFloat(value, 64)
	if floatErr != nil || math.IsNaN(floatValue) || math.IsInf(floatValue, 0) || math.Trunc(floatValue) != floatValue {
		return 0, fmt.Errorf("invalid integer %q", value)
	}
	return strconv.ParseInt(strconv.FormatFloat(floatValue, 'f', -1, 64), 10, bitSize)
}

func parseCompatibleUint(value string, bitSize int) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, bitSize)
	if err == nil {
		return parsed, nil
	}
	floatValue, floatErr := strconv.ParseFloat(value, 64)
	if floatErr != nil || math.IsNaN(floatValue) || math.IsInf(floatValue, 0) || floatValue < 0 || math.Trunc(floatValue) != floatValue {
		return 0, fmt.Errorf("invalid unsigned integer %q", value)
	}
	return strconv.ParseUint(strconv.FormatFloat(floatValue, 'f', -1, 64), 10, bitSize)
}

// ConfigToMap 将配置对象转换为map（导出函数）
func ConfigToMap(config any) (map[string]string, error) {
	return configToMap(config)
}

// UpdateConfigFromMap 从map更新配置对象（导出函数）
func UpdateConfigFromMap(config any, configMap map[string]string) error {
	return updateConfigFromMap(config, configMap)
}

// ExportAllConfigs 导出所有已注册的配置为扁平结构
func (cm *ConfigManager) ExportAllConfigs() map[string]string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	result := make(map[string]string)

	for name, cfg := range cm.configs {
		configMap, err := ConfigToMap(cfg)
		if err != nil {
			continue
		}

		// 使用 "模块名.配置项" 的格式添加到结果中
		for key, value := range configMap {
			result[name+"."+key] = value
		}
	}

	return result
}
