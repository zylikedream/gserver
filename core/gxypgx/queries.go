package gxypgx

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// InsertOne inserts a single row
func (p *PGXApp) InsertOne(ctx context.Context, tableName string, columns []string, values []interface{}) error {
	query := p.buildInsertQuery(tableName, columns)
	_, err := p.pool.Exec(ctx, query, values...)
	return err
}

// FindOne queries a single row with support for db:"inline" embedded structs and JSONB columns
func (p *PGXApp) FindOne(ctx context.Context, tableName string, dest interface{}, where string, args ...interface{}) error {
	// Get all fields including embedded structs with db:"inline"
	fields := p.getStructFields(dest)

	// Build column list
	var colNames []string
	for _, f := range fields {
		colNames = append(colNames, f.dbName)
	}

	// Build SELECT query with specific columns
	query := "SELECT " + strings.Join(colNames, ", ") + " FROM " + tableName + " WHERE " + where
	row := p.pool.QueryRow(ctx, query, args...)

	// Create a map to hold scanned values
	values := make([]interface{}, len(fields))
	valuePtrs := make([]interface{}, len(fields))

	for i, f := range fields {
		if f.isJSONB {
			// Use []byte for JSONB columns, will unmarshal manually
			var jsonBytes []byte
			values[i] = jsonBytes
			valuePtrs[i] = &values[i]
		} else {
			values[i] = reflect.New(f.fieldType).Interface()
			valuePtrs[i] = &values[i]
		}
	}

	err := row.Scan(valuePtrs...)
	if err != nil {
		return err
	}

	// Populate the dest struct
	return p.scanIntoStruct(dest, fields, values)
}

// getStructFields returns all fields including embedded structs with db:"inline"
func (p *PGXApp) getStructFields(v interface{}) []structField {
	var fields []structField
	typ := reflect.TypeOf(v)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return fields
	}

	p.collectFields(typ, &fields)
	return fields
}

// collectFields recursively collects fields from struct and embedded structs with db:"inline"
func (p *PGXApp) collectFields(typ reflect.Type, fields *[]structField) {
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}

		// Check if this is an embedded struct with db:"inline"
		if field.Anonymous && field.Tag.Get("db") == "inline" {
			// Recursively collect fields from embedded struct
			p.collectFields(field.Type, fields)
			continue
		}

		dbTag := field.Tag.Get("db")
		if dbTag == "" || dbTag == "-" {
			continue
		}

		sf := structField{
			fieldName: field.Name,
			dbName:    dbTag,
			fieldType: field.Type,
			isJSONB:   p.isJSONBType(field),
		}
		*fields = append(*fields, sf)
	}
}

type structField struct {
	fieldName string
	dbName    string
	fieldType reflect.Type
	isJSONB   bool
}

// isJSONBType checks if a field type should be stored as JSONB
func (p *PGXApp) isJSONBType(field reflect.StructField) bool {
	fieldType := field.Type

	// Check for map types
	if fieldType.Kind() == reflect.Map {
		return true
	}

	// Check for slices of pointers to structs (like []*Something)
	if fieldType.Kind() == reflect.Slice {
		elemType := fieldType.Elem()
		if elemType.Kind() == reflect.Ptr && elemType.Elem().Kind() == reflect.Struct {
			return true
		}
		// Also handle []Something (non-pointer slice of structs)
		if elemType.Kind() == reflect.Struct {
			return true
		}
	}

	return false
}

// scanIntoStruct populates the dest struct with scanned values
func (p *PGXApp) scanIntoStruct(dest interface{}, fields []structField, values []interface{}) error {
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() == reflect.Ptr {
		destVal = destVal.Elem()
	}

	destTyp := destVal.Type()

	// Build a map of db tag names to field indices in the dest struct
	dbTagToIndex := make(map[string]int)
	for i := 0; i < destTyp.NumField(); i++ {
		field := destTyp.Field(i)
		dbTag := field.Tag.Get("db")
		if dbTag != "" && dbTag != "-" {
			dbTagToIndex[dbTag] = i
		}
	}

	for i, sf := range fields {
		fieldIdx, ok := dbTagToIndex[sf.dbName]
		if !ok {
			continue
		}

		destField := destVal.Field(fieldIdx)

		if sf.isJSONB {
			// Handle JSONB deserialization
			jsonBytes, ok := values[i].([]byte)
			if ok && len(jsonBytes) > 0 {
				if err := json.Unmarshal(jsonBytes, destField.Addr().Interface()); err != nil {
					return err
				}
			}
		} else {
			// Direct assignment for non-JSONB types
			val := reflect.ValueOf(values[i])
			if val.Type().AssignableTo(destField.Type()) {
				destField.Set(val)
			} else if val.Type().ConvertibleTo(destField.Type()) {
				destField.Set(val.Convert(destField.Type()))
			}
		}
	}

	return nil
}

// Update updates data
func (p *PGXApp) Update(ctx context.Context, tableName string, set string, where string, args ...interface{}) error {
	query := "UPDATE " + tableName + " SET " + set + " WHERE " + where
	_, err := p.pool.Exec(ctx, query, args...)
	return err
}

// Delete deletes data
func (p *PGXApp) Delete(ctx context.Context, tableName string, where string, args ...interface{}) error {
	query := "DELETE FROM " + tableName + " WHERE " + where
	_, err := p.pool.Exec(ctx, query, args...)
	return err
}

// UpsertOne inserts or updates a row using INSERT ON CONFLICT DO UPDATE
func (p *PGXApp) UpsertOne(ctx context.Context, tableName string, modState interface{}, where string, args ...interface{}) error {
	// Get all fields from the modState
	fields := p.getStructFields(modState)

	// Build column list and value placeholders
	var columns []string
	var placeholders []string
	var setClauses []string
	var values []interface{}

	paramIndex := 1
	for _, f := range fields {
		columns = append(columns, f.dbName)

		if f.isJSONB {
			// Serialize JSONB field
			fieldVal := p.getFieldValue(modState, f.fieldName)
			jsonData, err := json.Marshal(fieldVal)
			if err != nil {
				return fmt.Errorf("failed to marshal JSONB field %s: %w", f.dbName, err)
			}
			values = append(values, jsonData)
			placeholders = append(placeholders, fmt.Sprintf("$%d", paramIndex))
			paramIndex++
		} else {
			fieldVal := p.getFieldValue(modState, f.fieldName)
			values = append(values, fieldVal)
			placeholders = append(placeholders, fmt.Sprintf("$%d", paramIndex))
			paramIndex++
		}

		// Build SET clause for ON CONFLICT UPDATE (exclude role_id from update)
		if f.dbName != "role_id" {
			setClauses = append(setClauses, fmt.Sprintf("%s = EXCLUDED.%s", f.dbName, f.dbName))
		}
	}

	// Build INSERT query
	insertQuery := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	// Build ON CONFLICT clause
	onConflict := fmt.Sprintf("ON CONFLICT (role_id) DO UPDATE SET %s", strings.Join(setClauses, ", "))

	// Combine INSERT and ON CONFLICT
	query := insertQuery + " " + onConflict

	_, err := p.pool.Exec(ctx, query, values...)
	return err
}

// getFieldValue gets the value of a field by name from the struct
func (p *PGXApp) getFieldValue(v interface{}, fieldName string) interface{} {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}

	field := rv.FieldByName(fieldName)
	if !field.IsValid() {
		return nil
	}

	// Handle interface types
	if field.Kind() == reflect.Interface && !field.IsNil() {
		return field.Elem().Interface()
	}

	return field.Interface()
}

// buildInsertQuery builds INSERT statement (placeholders format $1, $2, ...)
func (p *PGXApp) buildInsertQuery(tableName string, columns []string) string {
	var query strings.Builder
	placeholderCount := len(columns)

	query.WriteString("INSERT INTO ")
	query.WriteString(tableName)
	query.WriteString(" (")

	for i, col := range columns {
		if i > 0 {
			query.WriteString(", ")
		}
		query.WriteString(col)
	}

	query.WriteString(") VALUES (")

	for i := 0; i < placeholderCount; i++ {
		if i > 0 {
			query.WriteString(", ")
		}
		// pgx uses $1, $2, ... format (1-indexed)
		query.WriteString(fmt.Sprintf("$%d", i+1))
	}

	query.WriteString(")")
	return query.String()
}
