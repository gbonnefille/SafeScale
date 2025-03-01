#!/bin/bash
#
# Copyright 2018-2021, CS Systemes d'Information, http://csgroup.eu
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#{{.Revision}}

{{.Header}}

function print_error() {
    read line file <<<$(caller)
    echo "An error occurred in line $line of file $file:" "{"`sed "${line}q;d" "$file"`"}" >&2
    {{.ExitOnError}}
}
trap print_error ERR

function fail() {
    echo "PROVISIONING_ERROR: $1"
    echo -n "$1,${LINUX_KIND},${VERSION_ID},$(hostname),$(date +%Y/%m/%d-%H:%M:%S)" >/opt/safescale/var/state/user_data.final.done
	(sync; echo 3 > /proc/sys/vm/drop_caches; sleep 2) || true
    # For compatibility with previous user_data implementation (until v19.03.x)...
    ln -s ${SF_VARDIR}/state/user_data.final.done /var/tmp/user_data.done
    exit $1
}

# Redirects outputs to /opt/safescale/var/log/user_data.final.log
LOGFILE=/opt/safescale/var/log/user_data.final.log

### All output to one file and all output to the screen
exec > >(tee -a ${LOGFILE} /var/log/ss.log) 2>&1
set -x

# Tricks BashLibrary's waitUserData to believe the current phase 'user_data.final' is already done (otherwise will deadlock)
>/opt/safescale/var/state/user_data.final.done
# Includes the BashLibrary
{{ .BashLibrary }}
rm -f /opt/safescale/var/state/user_data.final.done

function install_drivers_nvidia() {
    lspci | grep -i nvidia &>/dev/null || return 0

    case $LINUX_KIND in
        ubuntu)
            sfFinishPreviousInstall
            add-apt-repository -y ppa:graphics-drivers &>/dev/null
            sfApt update || fail 201
            sfApt -y install nvidia-410 &>/dev/null || {
                sfApt -y install nvidia-driver-410 &>/dev/null || fail 201
            }
            ;;

        debian)
            if [ ! -f /etc/modprobe.d/blacklist-nouveau.conf ]; then
                echo -e "blacklist nouveau\nblacklist lbm-nouveau\noptions nouveau modeset=0\nalias nouveau off\nalias lbm-nouveau off" >>/etc/modprobe.d/blacklist-nouveau.conf
                rmmod nouveau
            fi
            sfWaitForApt && apt update &>/dev/null
            sfWaitForApt && apt install -y dkms build-essential linux-headers-$(uname -r) gcc make &>/dev/null || fail 202
            dpkg --add-architecture i386 &>/dev/null
            sfWaitForApt && apt update &>/dev/null
            sfWaitForApt && apt install -y lib32z1 lib32ncurses5 &>/dev/null || fail 203
            wget http://us.download.nvidia.com/XFree86/Linux-x86_64/410.78/NVIDIA-Linux-x86_64-410.78.run &>/dev/null || fail 204
            bash NVIDIA-Linux-x86_64-410.78.run -s || fail 205
            ;;

        redhat|centos)
            if [ ! -f /etc/modprobe.d/blacklist-nouveau.conf ]; then
                echo -e "blacklist nouveau\noptions nouveau modeset=0" >>/etc/modprobe.d/blacklist-nouveau.conf
                dracut --force
                rmmod nouveau
            fi
            yum -y -q install kernel-devel.$(uname -i) kernel-headers.$(uname -i) gcc make &>/dev/null || fail 206
            wget http://us.download.nvidia.com/XFree86/Linux-x86_64/410.78/NVIDIA-Linux-x86_64-410.78.run || fail 207
            # if there is a version mismatch between kernel sources and running kernel, building the driver would require 2 reboots to get it done, right now this is unsupported
            if [ $(uname -r) == $(yum list installed | grep kernel-headers | awk {'print $2'}).$(uname -i) ]; then
                bash NVIDIA-Linux-x86_64-410.78.run -s || fail 208
            fi
            rm -f NVIDIA-Linux-x86_64-410.78.run
            ;;
        *)
            echo "PROVISIONING_ERROR: Unsupported Linux distribution '$LINUX_KIND'!"
            fail 209
            ;;
    esac
}

function install_python3() {
    case $LINUX_KIND in
    debian|ubuntu)
        sfFinishPreviousInstall || fail 200
        sfApt update || fail 201
        sfApt -y install python3 &>/dev/null|| fail 210
        ;;
    redhat|rhel|centos)
        yum install -y python3 || fail 210
        ;;
    *)
        echo "PROVISIONING_ERROR: Unsupported Linux distribution '$LINUX_KIND'!"
        fail 209
        ;;
    esac
}

# ---- Main

install_drivers_nvidia
install_python3

echo -n "0,linux,${LINUX_KIND},${VERSION_ID},$(hostname),$(date +%Y/%m/%d-%H:%M:%S)" >/opt/safescale/var/state/user_data.final.done
# For compatibility with previous user_data implementation (until v19.03.x)...
ln -s ${SF_VARDIR}/state/user_data.final.done /var/tmp/user_data.done

(sync; echo 3 > /proc/sys/vm/drop_caches; sleep 2) || true

set +x
exit 0
