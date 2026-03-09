// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package liquidapi

import "github.com/sapcc/go-bits/regexpext"

// The following list of regexes is derived from a hardcoded list of valid values
// for an image's "vmware_ostype" attribute in Nova/Glance. This list was copied from
// https://github.com/openstack/nova/blob/master/nova/virt/vmwareapi/constants.py
// and the comment over there says:
//
//	This list was extracted from a file on an installation of ESX 6.5. The
//	file can be found in /usr/lib/vmware/hostd/vimLocale/en/gos.vmsg
//	The contents of this list should be updated whenever there is a new
//	release of ESX.
//
// As the list is over 4 years old, new os_type versions have broken our
// OSTypeProber. Therefore, we replaced all obvious version numbers in the valid
// values with placeholders, so that this does not break as easily in the future.
// We do not recommend utilizing this regex list for anything else other than
// validating the "vmware_ostype" attribute on existing images.

var isValidVMwareOSTypeRegex = map[regexpext.BoundedRegexp]bool{
	`almalinux_64Guest`:           true,
	`amazonlinux(\d+)_64Guest`:    true,
	`asianux(\d+)_64Guest`:        true,
	`asianux(\d+)Guest`:           true,
	`centos64Guest`:               true,
	`centos(\d+)_64Guest`:         true,
	`centos(\d+)Guest`:            true,
	`centosGuest`:                 true,
	`coreos64Guest`:               true,
	`crxPod1Guest`:                true,
	`crxSys1Guest`:                true,
	`darwin(\d+)_64Guest`:         true,
	`darwin(\d+)Guest`:            true,
	`darwin64Guest`:               true,
	`darwinGuest`:                 true,
	`debian(\d+)_64Guest`:         true,
	`debian(\d+)Guest`:            true,
	`dosGuest`:                    true,
	`eComStation2Guest`:           true,
	`eComStationGuest`:            true,
	`fedora64Guest`:               true,
	`fedoraGuest`:                 true,
	`freebsd(\d+)_64Guest`:        true,
	`freebsd(\d+)Guest`:           true,
	`freebsd64Guest`:              true,
	`freebsdGuest`:                true,
	`mandrakeGuest`:               true,
	`mandriva64Guest`:             true,
	`mandrivaGuest`:               true,
	`netware(\d+)Guest`:           true,
	`nld9Guest`:                   true,
	`oesGuest`:                    true,
	`openServer(\d+)Guest`:        true,
	`opensuse64Guest`:             true,
	`opensuseGuest`:               true,
	`oracleLinux(\d+)_64Guest`:    true,
	`oracleLinux64Guest`:          true,
	`oracleLinux(\d+)Guest`:       true,
	`oracleLinuxGuest`:            true,
	`os2Guest`:                    true,
	`other(\d+)xLinux64Guest`:     true,
	`other(\d+)xLinuxGuest`:       true,
	`otherGuest64`:                true,
	`otherGuest`:                  true,
	`otherLinux64Guest`:           true,
	`otherLinuxGuest`:             true,
	`redhatGuest`:                 true,
	`rhel(\d+)_64Guest`:           true,
	`rhel(\d+)Guest`:              true,
	`rockylinux_64Guest`:          true,
	`sjdsGuest`:                   true,
	`sles(\d+)_64Guest`:           true,
	`sles(\d+)Guest`:              true,
	`sles64Guest`:                 true,
	`slesGuest`:                   true,
	`solaris(\d+)_64Guest`:        true,
	`solaris(\d+)Guest`:           true,
	`suse64Guest`:                 true,
	`suseGuest`:                   true,
	`turboLinux64Guest`:           true,
	`turboLinuxGuest`:             true,
	`ubuntu64Guest`:               true,
	`ubuntuGuest`:                 true,
	`unixWare7Guest`:              true,
	`vmkernel(\d+)Guest`:          true,
	`vmkernelGuest`:               true,
	`vmwarePhoton64Guest`:         true,
	`win(\d+)AdvServGuest`:        true,
	`win(\d+)ProGuest`:            true,
	`win(\d+)ServGuest`:           true,
	`win(\d+)Guest`:               true,
	`windows(\d+)_64Guest`:        true,
	`windows(\d+)srv_64Guest`:     true,
	`windows(\d+)srvNext_64Guest`: true,
	`windows(\d+)Guest`:           true,
	`windows(\d+)Server64Guest`:   true,
	`winLonghorn64Guest`:          true,
	`winLonghornGuest`:            true,
	`winNetBusinessGuest`:         true,
	`winNetDatacenter64Guest`:     true,
	`winNetDatacenterGuest`:       true,
	`winNetEnterprise64Guest`:     true,
	`winNetEnterpriseGuest`:       true,
	`winNetStandard64Guest`:       true,
	`winNetStandardGuest`:         true,
	`winNetWebGuest`:              true,
	`winNTGuest`:                  true,
	`winVista64Guest`:             true,
	`winVistaGuest`:               true,
	`winXPHomeGuest`:              true,
	`winXPPro64Guest`:             true,
	`winXPProGuest`:               true,
}
