////////////////////////////////////////////////////////////////////////////////
// Copyright © 2019 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

package conf

type Permissioning struct {
	Paths            Paths
	Address          string
	RegistrationCode string `yaml:"registrationCode"`
	PublicKey        string
}
