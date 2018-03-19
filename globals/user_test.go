////////////////////////////////////////////////////////////////////////////////
// Copyright © 2018 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

package globals

import (
	"fmt"
	"testing"
)

// TestUserRegistry tests the constructors/getters/setters
// surrounding the User struct and the UserRegistry interface
func TestUserRegistry(t *testing.T) {

	// Loop from userDatabase.go to create and add users
	nickList := []string{"David", "Jim", "Ben", "Rick", "Spencer", "Jake",
		"Mario", "Will", "Sydney", "Jon0"}

	for i := 1; i <= NUM_DEMO_USERS; i++ {
		u := Users.NewUser("")
		u.Nick = nickList[i-1]
		//u.HUID = uint64(2*i)
		Users.UpsertUser(u)
	}

	// TESTS Start here
	test := 8
	pass := 0

	numUsers := Users.CountUsers()

	if numUsers != NUM_DEMO_USERS {
		t.Errorf("Count users is not working correctly")
	} else {
		pass++
	}

	usr9, exists := Users.GetUser(9)

	if usr9 == nil {
		t.Errorf("Error fetching user!")
	} else {
		pass++
	}

	getUser, exists := Users.GetUser(usr9.ID)

	if !exists || getUser.ID != usr9.ID {
		t.Errorf("GetUser: Returned unexpected result for user lookup!")
	}

	usr3, _ := Users.GetUser(3)
	usr5, _ := Users.GetUser(5)

	if usr3.Transmission.BaseKey == nil {
		t.Errorf("Error Setting the Transmission Base Key")
	} else {
		pass++
	}

	if usr3.Reception.BaseKey == usr5.Reception.BaseKey {
		t.Errorf("Transmissions keys are the same and they should be different!")
	} else {
		pass++
	}

	ids, _ := Users.GetNickList()

	if len(ids) != Users.CountUsers() {
		t.Errorf("Nicklist is not ok! ")
	} else {
		pass++
	}

	Users.DeleteUser(usr9.ID)

	if Users.CountUsers() != NUM_DEMO_USERS-1 {
		t.Errorf("User has not been deleted correctly.")
	} else {
		pass++
	}

	if _, userExists := Users.GetUser(usr9.ID); userExists {
		t.Errorf("DeleteUser: Excepted zero value for deleted user lookup!")
	} else {
		pass++
	}

	// HERE IT STOPS WORKING!
	_, ok := Users.LookupUser(usr3.HUID)

	if !ok {
		t.Errorf("Error Looking up user")
	} else {
		pass++
	}

	fmt.Println("Testing")
	fmt.Println(usr3.HUID)

	t_usr, _ := Users.GetUser(8)
	fmt.Println(t_usr.HUID)

	fmt.Println(Users.LookupUser(t_usr.HUID))

	fmt.Println("Checkpoint")

	println("User Test", pass, "out of", test, "tests passed.")
}
