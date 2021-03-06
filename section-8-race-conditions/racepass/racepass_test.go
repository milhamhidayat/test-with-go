package racepass

import (
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"

	env "test-with-go/envjson"

	_ "github.com/lib/pq"
)

func getPostgresConnection() string {
	host := os.Getenv("HOST")
	port := os.Getenv("PORT")
	username := os.Getenv("USERNAME")
	password := os.Getenv("PASSWORD")
	pqConnection := fmt.Sprintf("host=%s port=%s user=%s sslmode=disable password=%s", host, port, username, password)
	return pqConnection
}

func getDbConnection() string {
	host := os.Getenv("HOST")
	dbName := os.Getenv("DB")
	port := os.Getenv("PORT")
	username := os.Getenv("USERNAME")
	password := os.Getenv("PASSWORD")
	dbConnection := fmt.Sprintf("host=%s dbname=%s port=%s user=%s sslmode=disable password=%s", host, dbName, port, username, password)
	return dbConnection
}

func TestMain(m *testing.M) {
	err := env.Setup()
	if err != nil {
		panic(fmt.Errorf(err.Error()))
	}
	// 0. flag.Parse() if you need flags
	exitCode := run(m)
	os.Exit(exitCode)
}

func run(m *testing.M) int {
	const (
		dropDB          = `DROP DATABASE IF EXISTS test_user_store;`
		createDB        = `CREATE DATABASE test_user_store;`
		createUserTable = `CREATE TABLE users (
												 id SERIAL PRIMARY KEY,
												 name TEXT,
												 email TEXT UNIQUE NOT NULL,
												 balance INTEGER
											 );`
	)

	pqConnection := getPostgresConnection()
	psql, err := sql.Open("postgres", pqConnection)
	if err != nil {
		panic(fmt.Errorf("sql.Open() err = %s", err))
	}
	defer psql.Close()

	_, err = psql.Exec(dropDB)
	if err != nil {
		panic(fmt.Errorf("psql.Exec() err = %s", err))
	}
	_, err = psql.Exec(createDB)
	if err != nil {
		panic(fmt.Errorf("psql.Exec() err = %s", err))
	}
	// teardown
	defer func() {
		_, err = psql.Exec(dropDB)
		if err != nil {
			panic(fmt.Errorf("psql.Exec() err = %s", err))
		}
	}()

	dbConnection := getDbConnection()
	db, err := sql.Open("postgres", dbConnection)
	if err != nil {
		panic(fmt.Errorf("sql.Open() err = %s", err))
	}
	defer db.Close()
	_, err = db.Exec(createUserTable)
	if err != nil {
		panic(fmt.Errorf("db.Exec() err = %s", err))
	}

	return m.Run()
}

type racyUserStore struct {
	UserStore
	wg *sync.WaitGroup
}

func (rus *racyUserStore) Find(id int) (*User, error) {
	user, err := rus.UserStore.Find(id)
	if err != nil {
		return nil, err
	}
	rus.wg.Done()
	rus.wg.Wait()
	return user, err
}

func TestSpend_race(t *testing.T) {
	dbConnection := getDbConnection()
	db, err := sql.Open("postgres", dbConnection)
	if err != nil {
		panic(fmt.Errorf("sql.Open() err = %s", err))
	}
	defer db.Close()
	us := &PsqlUserStore{
		tx:  db,
		sql: db,
	}
	jon := &User{
		Name:    "Jon Calhoun",
		Email:   "jon@calhoun.io",
		Balance: 100,
	}
	err = us.Create(jon)
	if err != nil {
		t.Errorf("us.Create() err = %s", err)
	}
	defer func() {
		err := us.Delete(jon.ID)
		if err != nil {
			t.Errorf("us.Delete() err = %s", err)
		}
	}()

	rus := &racyUserStore{
		UserStore: us,
		wg:        &sync.WaitGroup{},
	}
	rus.wg.Add(2)
	var spendWg sync.WaitGroup
	for i := 0; i < 2; i++ {
		spendWg.Add(1)
		go func() {
			err := Spend(rus, jon.ID, 25)
			if err != nil {
				t.Fatalf("Spend() err = %s", err)
			}
			spendWg.Done()
		}()
	}
	spendWg.Wait()
	got, err := us.Find(jon.ID)
	if err != nil {
		t.Fatalf("us.Find() err = %s", err)
	}
	if got.Balance != 50 {
		t.Fatalf("user.Balance = %d; want %d", got.Balance, 50)
	}
}
