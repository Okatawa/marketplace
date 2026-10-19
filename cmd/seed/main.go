package main

import (
	"bufio"
	"example.com/marketplace/internal/demo"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	base := flag.String("base-url", "http://127.0.0.1:8080", "gateway URL")
	flag.Parse()
	password := os.Getenv("DEMO_PASSWORD")
	if password == "" {
		f, e := os.Open(".local/.env")
		if e == nil {
			defer f.Close()
			s := bufio.NewScanner(f)
			for s.Scan() {
				k, v, ok := strings.Cut(s.Text(), "=")
				if ok && k == "DEMO_PASSWORD" {
					password = v
				}
			}
		}
	}
	if len(password) < 12 {
		fmt.Fprintln(os.Stderr, "Set DEMO_PASSWORD (12+ characters) in .local/.env")
		os.Exit(1)
	}
	seller := demo.New(*base)
	if e := seller.Account("Анна", "anna@example.com", password); e != nil {
		panic(e)
	}
	buyer := demo.New(*base)
	if e := buyer.Account("Борис", "boris@example.com", password); e != nil {
		panic(e)
	}
	d, _, e := seller.Do("GET", "/me/listings?limit=100", nil)
	if e != nil {
		panic(e)
	}
	existing := map[string]bool{}
	for _, item := range d["items"].([]any) {
		existing[item.(map[string]any)["title"].(string)] = true
	}
	titles := []string{"Горный велосипед", "Настольная лампа", "Виниловый проигрыватель", "Удобное кресло", "Фотоаппарат для путешествий", "Рюкзак для выходных", "Кофемолка", "Наушники"}
	categories := []string{"sport", "home", "electronics", "home", "electronics", "sport", "home", "electronics"}
	prices := []int{1500000, 250000, 1200000, 800000, 2400000, 350000, 400000, 550000}
	for i, title := range titles {
		if existing[title] {
			continue
		}
		l, _, e := seller.Do("POST", "/listings", map[string]any{"title": title, "description": "Бережно использовали, всё работает. Можно посмотреть при встрече. Напишите, чтобы договориться о деталях.", "category_id": categories[i], "price_minor": prices[i]})
		if e != nil {
			panic(e)
		}
		id := l["id"].(string)
		if e = seller.Upload(id, demo.Photo(i)); e != nil {
			panic(e)
		}
		l, _, e = seller.Do("GET", "/listings/"+id, nil)
		if e != nil {
			panic(e)
		}
		if _, _, e = seller.Do("POST", "/listings/"+id+"/publish", map[string]any{"expected_version": l["version"]}); e != nil {
			panic(e)
		}
	}
	fmt.Println("Demo users and listings ready. Credentials: anna@example.com / boris@example.com; password in .local/.env (DEMO_PASSWORD).")
}
