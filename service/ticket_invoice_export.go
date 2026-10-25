package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"eflow.local/new-api/model"
)

var ErrInvoiceExportCellTooLong = errors.New("An invoice field exceeds the Excel cell limit")

func ExportTicketInvoices(ctx context.Context, ids []int) ([]byte, error) {
	records, err := model.ReadInvoiceExport(ctx, ids)
	if err != nil {
		return nil, err
	}
	return buildInvoiceWorkbook(records)
}

var invoiceExcelEscape = regexp.MustCompile(`_x[0-9A-Fa-f]{4}_`)

// This workbook has one fixed sheet: all user-controlled fields are inline
// strings (never formulas), while the CNY amount is a two-decimal numeric cell.
func buildInvoiceWorkbook(records []model.InvoiceExportRecord) ([]byte, error) {
	headers := []string{"工单号", "申请人ID", "申请人", "工单主题", "发票抬头", "税号", "开户行", "银行账号", "注册地址", "联系电话", "接收邮箱", "申请金额（元）", "关联订单ID", "关联订单号", "申请说明（含备注）", "工单状态", "发票状态", "申请时间（北京时间）", "开具时间（北京时间）"}
	var sheet bytes.Buffer
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols><col min="1" max="19" width="22" customWidth="1"/><col min="15" max="15" width="60" customWidth="1"/></cols><sheetData>`)
	writeRow := func(values []string, row int, money bool) error {
		fmt.Fprintf(&sheet, `<row r="%d">`, row)
		for col, value := range values {
			ref := fmt.Sprintf("%c%d", 'A'+col, row)
			if money && col == 11 {
				fmt.Fprintf(&sheet, `<c r="%s" s="1"><v>%s</v></c>`, ref, value)
				continue
			}
			if len(utf16.Encode([]rune(value))) > 32767 {
				return ErrInvoiceExportCellTooLong
			}
			style := 0
			if row == 1 {
				style = 2
			}
			fmt.Fprintf(&sheet, `<c r="%s" t="inlineStr" s="%d"><is><t xml:space="preserve">`, ref, style)
			// Preserve strings that resemble Excel's own escaped control sequences.
			value = invoiceExcelEscape.ReplaceAllStringFunc(value, func(match string) string { return "_x005F_" + match[1:] })
			var text strings.Builder
			for _, char := range value {
				if char < 32 && char != '\n' && char != '\t' && char != '\r' {
					fmt.Fprintf(&text, "_x%04X_", char)
				} else {
					text.WriteRune(char)
				}
			}
			if err := xml.EscapeText(&sheet, []byte(text.String())); err != nil {
				return err
			}
			sheet.WriteString(`</t></is></c>`)
		}
		sheet.WriteString(`</row>`)
		return nil
	}
	if err := writeRow(headers, 1, false); err != nil {
		return nil, err
	}
	beijing := time.FixedZone("Asia/Shanghai", 8*60*60)
	formatTime := func(timestamp int64) string {
		if timestamp <= 0 {
			return ""
		}
		return time.Unix(timestamp, 0).In(beijing).Format("2006-01-02 15:04:05")
	}
	ticketStatuses := map[int]string{1: "待处理", 2: "处理中", 3: "已解决", 4: "已关闭"}
	invoiceStatuses := map[int]string{1: "待开具", 2: "已开具", 3: "已拒绝"}
	for index, record := range records {
		inv, ticket := record.Invoice, record.Ticket
		if math.IsNaN(inv.TotalMoney) || math.IsInf(inv.TotalMoney, 0) {
			return nil, model.ErrInvoiceExportIncomplete
		}
		values := []string{
			strconv.Itoa(ticket.Id), strconv.Itoa(ticket.UserId), ticket.Username, ticket.Subject,
			inv.CompanyName, inv.TaxNumber, inv.BankName, inv.BankAccount, inv.CompanyAddress, inv.CompanyPhone, inv.Email,
			strconv.FormatFloat(inv.TotalMoney, 'f', 2, 64), inv.TopUpOrderIds, strings.Join(record.OrderNumbers, "\n"), record.Application,
			ticketStatuses[ticket.Status], invoiceStatuses[inv.InvoiceStatus], formatTime(inv.CreatedTime), formatTime(inv.IssuedTime),
		}
		if err := writeRow(values, index+2, true); err != nil {
			return nil, err
		}
	}
	fmt.Fprintf(&sheet, `</sheetData><autoFilter ref="A1:S%d"/></worksheet>`, len(records)+1)
	files := []struct{ name, content string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="发票申请" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
		{"xl/styles.xml", `<?xml version="1.0" encoding="UTF-8"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="3"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment vertical="top" wrapText="1"/></xf><xf numFmtId="4" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`},
		{"xl/worksheets/sheet1.xml", sheet.String()},
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, file := range files {
		entry, err := archive.Create(file.name)
		if err != nil {
			return nil, err
		}
		if _, err = entry.Write([]byte(file.content)); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
