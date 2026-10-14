// Package sample builds the demo workbook: three linked sheets (Products,
// Customers, Orders) so a fresh install can feel the import loop without
// a file of its own. Deterministic: same seed, same file.
package sample

import (
	"fmt"
	"math/rand"
	"strconv"

	"github.com/xuri/excelize/v2"
)

func writeSheet(f *excelize.File, sheet string, rows [][]string) {
	if sheet != "Sheet1" {
		f.NewSheet(sheet)
	}
	for r, row := range rows {
		for c, v := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
			f.SetCellValue(sheet, cell, v)
		}
	}
}

// New returns the generated workbook.
func New() (*excelize.File, error) {
	f := excelize.NewFile()

	rng := rand.New(rand.NewSource(42))
	kinds := []string{"Coffee", "Tea", "Cocoa", "Bagel", "Muffin", "Cookie", "Croissant", "Syrup"}
	cats := []string{"drinks", "bakery", "snacks", "supplies"}
	adjectives := []string{"Golden", "Iced", "Honey", "Matcha", "Butter", "Spiced", "Fresh", "Dark", "Vanilla", "Maple"}

	products := [][]string{{"name", "category", "price", "status", "stock"}}
	for i := 1; i <= 140; i++ {
		products = append(products, []string{
			adjectives[rng.Intn(len(adjectives))] + " " + kinds[rng.Intn(len(kinds))] + " " + fmt.Sprintf("%03d", i),
			cats[rng.Intn(len(cats))],
			strconv.Itoa(20 + rng.Intn(480)),
			[]string{"active", "active", "active", "low", "sold out"}[rng.Intn(5)],
			strconv.Itoa(rng.Intn(200)),
		})
	}
	f.SetSheetName("Sheet1", "Products")
	writeSheet(f, "Products", products)

	firsts := []string{"Amy", "Bob", "Carla", "Dan", "Elle", "Fon", "Gift", "Hana", "Ivy", "Joe", "Kan", "Lek", "Mild", "Nok", "Oat", "Ploy", "Quinn", "Rin", "Som", "Tae", "Vee", "Win", "Yui", "Zack"}
	lasts := []string{"Smith", "Tanaka", "Lee", "Garcia", "Nguyen", "Brown", "Chen", "Patel", "Kim", "Rossi"}
	cities := []string{"Bangkok", "Chiang Mai", "Singapore", "Tokyo", "Taipei", "Jakarta", "Manila", "Hanoi"}

	customers := [][]string{{"name", "email", "point", "city"}}
	for i := 1; i <= 90; i++ {
		name := firsts[rng.Intn(len(firsts))] + " " + lasts[rng.Intn(len(lasts))]
		customers = append(customers, []string{
			name,
			fmt.Sprintf("user%03d@example.com", i),
			strconv.Itoa(rng.Intn(5000)),
			cities[rng.Intn(len(cities))],
		})
	}
	writeSheet(f, "Customers", customers)

	orderStatus := []string{"paid", "shipped", "delivered", "refunded"}
	orders := [][]string{{"order id", "customer", "product", "qty", "total", "date", "status"}}
	for i := 1; i <= 160; i++ {
		qty := 1 + rng.Intn(5)
		orders = append(orders, []string{
			fmt.Sprintf("ORD-%04d", 1000+i),
			customers[1+rng.Intn(len(customers)-1)][0],
			products[1+rng.Intn(len(products)-1)][0],
			strconv.Itoa(qty),
			strconv.Itoa(qty * (20 + rng.Intn(480))),
			fmt.Sprintf("2026-%02d-%02d", 1+rng.Intn(9), 1+rng.Intn(28)),
			orderStatus[rng.Intn(len(orderStatus))],
		})
	}
	writeSheet(f, "Orders", orders)

	return f, nil
}
