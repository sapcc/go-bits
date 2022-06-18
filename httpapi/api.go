/******************************************************************************
*
*  Copyright 2022 SAP SE
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package httpapi

import (
	"github.com/gorilla/mux"
	"github.com/rs/cors"
)

//API is the interface that applications can use to plug their own API
//endpoints into the http.Handler constructed by this package's Compose()
//function.
//
//In this package, some special API instances with names like "With..." and
//"Without..." are available that apply to the entire http.Handler returned by
//Compose(), instead of just adding endpoints to it.
type API interface {
	AddTo(r *mux.Router)
}

//A value that can appear as an argument of Compose() without actually being an
//API. The AddTo() implementation is empty; Compose() will call the provided
//configure() method instead.
type pseudoAPI struct {
	configure func(*middleware)
}

func (p pseudoAPI) AddTo(r *mux.Router) {
	//no-op, see above
}

//WithoutLogging can be given as an argument to Compose() to disable request
//logging for the entire http.Handler returned by Compose().
//
//This modifier is intended for use during unit tests.
func WithoutLogging() API {
	return pseudoAPI{
		configure: func(m *middleware) {
			m.skipAllLogs = true
		},
	}
}

//WithCORS can be given as an argument to Compose() to add the
//github.com/rs/cors middleware to the entire http.Handler returned by
//Compose().
func WithCORS(opts cors.Options) API {
	return pseudoAPI{
		configure: func(m *middleware) {
			m.inner = cors.New(opts).Handler(m.inner)
		},
	}
}
