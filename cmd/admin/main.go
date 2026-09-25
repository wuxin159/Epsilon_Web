package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"epsilon/internal/config"
	"epsilon/internal/storage"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config")
	flag.Parse()
	args := flag.Args()

	if len(args) == 0 {
		usage()
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	db, err := storage.Open(cfg.Database.Path)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := storage.Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	repo := storage.NewLicenseRepo(db)

	switch args[0] {
	case "add":
		if len(args) < 3 {
			usage()
		}
		days, err := strconv.Atoi(args[2])
		if err != nil {
			log.Fatalf("bad days %q: %v", args[2], err)
		}
		note := ""
		if len(args) >= 4 {
			note = args[3]
		}
		expireAt := time.Now().AddDate(0, 0, days).Unix()
		if err := repo.Upsert(args[1], expireAt, note); err != nil {
			log.Fatalf("upsert: %v", err)
		}
		fmt.Printf("OK  %s  expires=%s\n",
			args[1], time.Unix(expireAt, 0).Format("2006-01-02 15:04:05"))
	case "list":
		list, err := repo.List()
		if err != nil {
			log.Fatalf("list: %v", err)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "MACHINE_CODE\tEXPIRE_AT\tNOTE")
		for _, l := range list {
			fmt.Fprintf(w, "%s\t%s\t%s\n",
				l.MachineCode,
				time.Unix(l.ExpireAt, 0).Format("2006-01-02 15:04:05"),
				l.Note)
		}
		w.Flush()
	case "delete":
		if len(args) < 2 {
			usage()
		}
		if err := repo.Delete(args[1]); err != nil {
			log.Fatalf("delete: %v", err)
		}
		fmt.Println("OK")
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  admin add <machine_code> <days> [note]   add or extend a license
  admin list                                list all licenses
  admin delete <machine_code>               revoke a license`)
	os.Exit(1)
}
