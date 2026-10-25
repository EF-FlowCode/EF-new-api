package service

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"strings"
	"testing"

	"eflow.local/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestInvoiceWorkbookPreservesBillingData(t *testing.T) {
	records := []model.InvoiceExportRecord{{
		Ticket:       model.Ticket{Id: 9, UserId: 2, Username: "alice", Subject: "开票", Status: 4},
		Invoice:      model.TicketInvoice{CompanyName: "=1+1 & <研究所>", TaxNumber: "001234567890123456789", BankName: "开户行", BankAccount: "000123456789012345678", CompanyAddress: "地址", CompanyPhone: "01012345678", Email: "invoice@example.test", TotalMoney: 123.4, TopUpOrderIds: "[7,8]", InvoiceStatus: 2, CreatedTime: 1, IssuedTime: 2},
		OrderNumbers: []string{"0007", "0008"}, Application: "备注：拆票\n_x0041_\x01",
	}}
	data, err := buildInvoiceWorkbook(records)
	require.NoError(t, err)
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	require.Len(t, archive.File, 6)
	var sheetData []byte
	for _, file := range archive.File {
		reader, err := file.Open()
		require.NoError(t, err)
		raw, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		decoder := xml.NewDecoder(bytes.NewReader(raw))
		for {
			_, err := decoder.Token()
			if err == io.EOF {
				break
			}
			require.NoError(t, err, file.Name)
		}
		if file.Name == "xl/worksheets/sheet1.xml" {
			sheetData = raw
		}
	}
	var sheet struct {
		Rows []struct {
			Cells []struct {
				Ref     string `xml:"r,attr"`
				Type    string `xml:"t,attr"`
				Style   string `xml:"s,attr"`
				Text    string `xml:"is>t"`
				Value   string `xml:"v"`
				Formula string `xml:"f"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	require.NoError(t, xml.Unmarshal(sheetData, &sheet))
	require.Len(t, sheet.Rows, 2)
	row := sheet.Rows[1].Cells
	require.Len(t, row, 19)
	require.Equal(t, "=1+1 & <研究所>", row[4].Text)
	require.Equal(t, "inlineStr", row[4].Type)
	require.Equal(t, "001234567890123456789", row[5].Text)
	require.Equal(t, "000123456789012345678", row[7].Text)
	require.Equal(t, "123.40", row[11].Value)
	require.Equal(t, "1", row[11].Style)
	require.Equal(t, "0007\n0008", row[13].Text)
	require.Equal(t, "备注：拆票\n_x005F_x0041__x0001_", row[14].Text)
	require.Equal(t, "已开具", row[16].Text)
	require.Equal(t, "1970-01-01 08:00:01", row[17].Text)
	for _, cell := range row {
		require.Empty(t, cell.Formula)
	}
	if fixture := os.Getenv("INVOICE_EXPORT_FIXTURE"); fixture != "" {
		require.NoError(t, os.WriteFile(fixture, data, 0600))
	}
	records[0].Application = strings.Repeat("长", 32768)
	_, err = buildInvoiceWorkbook(records)
	require.ErrorIs(t, err, ErrInvoiceExportCellTooLong, "never silently truncate invoice instructions")
}
