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

//Package httpapi contains opinionated base machinery for assembling and
//exposing an API consisting of HTTP endpoints.
//
//The core of the package interface is the Compose() method, which creates a
//single http.Handler serving any number of HTTP APIs, each implemented as a
//type satisfying this package's API interface.
//
//Compose() creates a single router that encompasses all API's endpoints, and
//adds a few middlewares on top that apply to all these endpoints.
//
//Logging
//
//For each HTTP request served through this package, a plain-text log line in a
//format similar to nginx's "combined" format is written using the logger from
//package logg (by default, to stderr) using the special log level "REQUEST".
//
//To suppress logging of specific requests, call SkipRequestLog() somewhere
//inside the handler.
package httpapi
