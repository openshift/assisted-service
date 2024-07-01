# Multipath
If you need to test something in an environment with multipath, there are two approaches to prepare the environment:

- Create VMs and configure it manually.
- Use test-infra.

Before starting, here's a brief overview for those unfamiliar with multipath:

Multipath refers to a technique or technology that allows for redundant physical connections between a server (initiator) and its storage devices (targets). This redundancy helps enhance reliability and availability by providing multiple paths (connections) between the server and storage. If one path fails or becomes unreliable, data can still be accessed through an alternative path.

iSCSI is a protocol that allows for the transport of data over IP networks. It enables the creation of a storage area network (SAN) by linking data storage facilities via high-speed IP networks, making remote storage appear as if it were locally attached.

To achieve multipath, you will:

1. Set up an iSCSI target, which involves configuring a storage device that can be accessed over the network.
2. Set up a server (initiator) to connect to this target.
3. Ensure there are more than one network paths between the initiator and the target.

![alt multipath](multipath.png)

## 1st approach

### STEP-1 - Connect to Your Machine Through VMM
In your virt-manager (used remotely to manage VMs on a physical server), connect to your beaker machine (some physical server you have).

### STEP-2 - Create SAN VM + configure iSCSI target

- Download the Fedora ISO to your Beaker machine (a quick Google search and use wget).
- In your VMM, create a new VM and select the ISO you downloaded, naming it "SAN". In the Network selection phase, keep the default network.
- Enable the root user and ssh
- After installation is complete, go to "Show virtual hardware details" -> "Add Hardware" -> "Storage" -> create a 120GB disk.
- SSH into your new VM and use `lsblk` to verify that you have a new block device in the list (likely named vdb). For example:
   ```
    root@san:~# lsblk
    NAME            MAJ:MIN RM  SIZE RO TYPE MOUNTPOINTS
    sr0              11:0    1    2K  0 rom  
    zram0           251:0    0  1.4G  0 disk [SWAP]
    vda             252:0    0   20G  0 disk 
    ├─vda1          252:1    0    1M  0 part 
    ├─vda2          252:2    0    1G  0 part /boot
    └─vda3          252:3    0   19G  0 part 
      └─fedora-root 253:0    0   15G  0 lvm  /
    vdb             252:16   0  120G  0 disk 
   ```
- Follow these instructions for setting up the iSCSI target (https://fedoraproject.org/wiki/Scsi-target-utils_Quickstart_Guide):
    - Installing the scsi-target-utils package:
      ```bash
      dnf -y install scsi-target-utils
      ```
    - Start the firewalld service:
      ```bash
      sudo systemctl start firewalld
      ```
    - Add the iSCSI target in the firewall configuration:
      ```bash
      firewall-cmd --zone=FedoraServer --add-service=iscsi-target --permanent
      ```
    - Reload the firewall configuration to apply the changes:
      ```bash
      firewall-cmd --reload
      ```
    - Starts the iSCSI target daemon:
      ```bash
      sudo systemctl start tgtd
      ```
    - Enable the tgtd service to start automatically at boot:
      ```bash
      sudo systemctl enable tgtd
      ```
    - Create a target device:
      ```bash
      tgtadm --lld iscsi --mode target --op new --tid=1 --targetname iqn.2009-02.com.example:<any name you want>
      ```
    - Add LUN to the target device:
      ```bash
      tgtadm --lld iscsi --mode logicalunit --op new --tid 1 --lun 1 -b /dev/vdb
      ```
    - Configured the target to accept ALL initiators:
      ```bash
      tgtadm --lld iscsi --mode target --op bind --tid 1 -I ALL
      ```
    - Show the current configuration and status of the iSCSI targets:
      ```bash
      sudo tgtadm --mode target --op show
      ```

### STEP-3 - Create SNO VM (the initiator)

- Download the ISO with SSH enabled from the Assisted Installer's UI.
- In your VMM, create a new VM, naming it "SNO-SAN". Select the ISO you downloaded and allocate 22GB of memory, 12 CPUs, and 120GB of storage.
- Discover (client):
   ```bash
    iscsiadm --mode discovery --type sendtargets --portal <SAN 1st IP>:3260
   ```
-  Login to iSCSI:
   ```bash
   iscsiadm -m node -T iqn.2009-02.com.example:<iSCSI target you created in SAN> -p <SAN 1st IP>:3260 -l
   ```
- Run `lsblk` to verify if an additional disk has been added. You can verify that the disk you see is the iSCSI target by using `dmesg`. For example:
  ```
    [root@sno-san ~]# lsblk
    NAME     MAJ:MIN RM    SIZE RO TYPE  MOUNTPOINTS
    loop0      7:0    0   10.5G  0 loop  /var/lib/containers/storage/overlay
                                         /var
                                         /etc
                                         /run/ephemeral
    loop1      7:1    0 1008.1M  1 loop  /usr
                                         /boot
                                         /
                                         /sysroot
    sda        8:0    0    120G  0 disk  
    sr0       11:0    1    1.1G  0 rom   /run/media/iso
    vda      252:0    0    120G  0 disk

    [root@sno-san ~]# dmesg
    ...
    [ 1450.028086] scsi host6: iSCSI Initiator over TCP/IP
    [ 1450.032801] scsi 6:0:0:0: RAID              IET      Controller       0001 PQ: 0 ANSI: 5
    [ 1450.033872] scsi 6:0:0:0: Attached scsi generic sg1 type 12
    [ 1450.034212] scsi 6:0:0:1: Direct-Access     IET      VIRTUAL-DISK     0001 PQ: 0 ANSI: 5
    [ 1450.035230] scsi 6:0:0:1: Attached scsi generic sg2 type 0
    [ 1450.046704] sd 6:0:0:1: Power-on or device reset occurred
    [ 1450.046889] sd 6:0:0:1: [sda] 251658240 512-byte logical blocks: (129 GB/120 GiB)
    [ 1450.046958] sd 6:0:0:1: [sda] Write Protect is off
    [ 1450.046960] sd 6:0:0:1: [sda] Mode Sense: 69 00 10 08
    [ 1450.047067] sd 6:0:0:1: [sda] Write cache: enabled, read cache: enabled, supports DPO and FUA
    [ 1450.059414] sd 6:0:0:1: [sda] Attached SCSI disk
    ```

### STEP-4 - 2nd Network as 2nd path to reach the iSCSI target

1. Create Isolated Network - In your VMM, right click on your beaker machine -> click on details -> Virtual Networks -> Add Network -> naming it “SAN-network”, mode "isolated" -> finish
2. In SAN VM - "show virtual hardware details" -> Add Hardware -> Network -> select the isolated network you created -> finish
3. In SNO-SAN VM - "show virtual hardware details" -> Add Hardware -> Network -> select the isolated network you created -> finish
4. In SNO-SAN VM -
    ```bash
    iscsiadm --mode discovery --type sendtargets --portal <SAN 2nd IP>:3260
    iscsiadm -m node -T iqn.2009-02.com.example:<iSCSI target you created in SAN> -p <SAN 2nd IP>:3260 -l
    ```
5. Run `multipath -ll` to confirm that multipathing is active.

    example output:
   ```
   [root@sno-san ~]# multipath -ll
    mpatha (360000000000000000e00000000010001) dm-0 IET,VIRTUAL-DISK
    size=120G features='0' hwhandler='0' wp=rw
    |-+- policy='service-time 0' prio=1 status=active
    | `- 6:0:0:1 sda 8:0  active ready running
    `-+- policy='service-time 0' prio=1 status=enabled
    `- 7:0:0:1 sdb 8:16 active ready running
    ```

## 2nd approach

In this approach, your beaker machine will act as the target, while the initiator will be the test-infra VM (running on the beaker machine).

1. SSH into your beaker machine and navigate to the directory where the test-infra is cloned.
2. Create a VM that includes two different networks:
   ```bash
   make deploy_nodes_with_networking MASTERS_COUNT=1
   ```
3. On the target side (beaker machine):
   - create disk on hypervisor:
     ```bash
     qemu-img create -o preallocation=full -f qcow2 /tmp/disk0 30G
     ```
   - Install tragetCli package:
     ```bash
     dnf install targetcli -y
     ```
     ```bash
     targetcli clearconfig  confirm=True
     targetcli backstores/fileio create name=disk0 size=30G file_or_dev=/tmp/disk0
      ```
   - Create a target:
     ```bash
     targetcli /iscsi create iqn.2023-01.com.example:target01
     ```
   - Create a LUN:
     ```bash
     targetcli /iscsi/iqn.2023-01.com.example:target01/tpg1/luns create /backstores/fileio/disk0
     ```
   - Create portal:
     ```bash
     targetcli /iscsi/iqn.2023-01.com.example:target01/tpg1/portals create <1st target IP>
     targetcli /iscsi/iqn.2023-01.com.example:target01/tpg1/portals create <2nd target IP>
     ```
   - Allow access to initiator:
     ```bash
     targetcli /iscsi/iqn.2023-01.com.example:target01/tpg1/acls create iqn.1994-05.com.redhat:e1c30923a3a
     targetcli / saveconfig
     ```
4. On the initiator side (test-infra VM):
   - SSH into the VM you created in step 2.
   - Discover (client):
     ```bash
     iscsiadm --mode discovery --type sendtargets --portal <1st target IP>
     iscsiadm --mode discovery --type sendtargets --portal <2nd target IP>
     ```
   - Login to iSCSI:
     ```bash
     iscsiadm -m node -T iqn.2023-01.com.example:target01 -p <1st target IP>:3260 -l
     iscsiadm -m node -T iqn.2023-01.com.example:target01 -p <2nd target IP>:3260 -l
     ```
   - Run `multipath -ll` to confirm that multipathing is active.