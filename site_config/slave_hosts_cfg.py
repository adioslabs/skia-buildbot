""" This file contains configuration information for the build slave host
machines. """


import collections
import json
import ntpath
import os
import posixpath
import sys


CHROMECOMPUTE_BUILDBOT_PATH = ['storage', 'skia-repo', 'buildbot']

# Indicates that this machine is not connected to a KVM switch.
NO_KVM_SWITCH = '(not on KVM)'
NO_KVM_NUM = '(not on KVM)'

# Indicates that this machine has no static IP address.
NO_IP_ADDR = '(no static IP)'

# Files to copy into buildslave checkouts.
CHROMEBUILD_COPIES = [
  {
    "source": ".bot_password",
    "destination": "build/site_config",
  },
]

KVM_SWITCH_DOOR = 'DOOR'   # KVM switch closest to the door.
KVM_SWITCH_OFFICE = 'OFFICE' # KVM switch closest to the office.

LAUNCH_SCRIPT_UNIX = ['scripts', 'skiabot-slave-start-on-boot.sh']
LAUNCH_SCRIPT_WIN = ['scripts', 'skiabot-slave-start-on-boot.bat']


# Data for all Skia build slave hosts.
_slave_host_dicts = {

################################ Linux Machines ################################

  'skiabot-shuttle-ubuntu13-xxx': {
    'slaves': [
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': '192.168.1.120',
    'kvm_switch': KVM_SWITCH_OFFICE,
    'kvm_num': 'H',
    'path_to_buildbot': ['buildbot'],
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

  'skia-vm-001': {
    'slaves': [
      ('skiabot-linux-swarm-112', '112', False),
      ('skiabot-linux-swarm-113', '113', False),
      ('skiabot-linux-swarm-114', '114', False),
      ('skiabot-linux-swarm-115', '115', False),
      ('skiabot-linux-swarm-116', '116', False),
      ('skiabot-linux-swarm-117', '117', False),
      ('skiabot-linux-swarm-118', '118', False),
      ('skiabot-linux-swarm-119', '119', False),
      ('skiabot-linux-swarm-120', '120', False),
      ('skiabot-linux-swarm-121', '121', False),
      ('skiabot-linux-swarm-122', '122', False),
      ('skiabot-linux-swarm-123', '123', False),
      ('skiabot-linux-swarm-124', '124', False),
      ('skiabot-linux-swarm-125', '125', False),
      ('skiabot-linux-swarm-126', '126', False),
      ('skiabot-linux-swarm-127', '127', False),
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': CHROMECOMPUTE_BUILDBOT_PATH,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

  'skia-vm-002': {
    'slaves': [
      ('skiabot-linux-swarm-128', '128', False),
      ('skiabot-linux-swarm-129', '129', False),
      ('skiabot-linux-swarm-130', '130', False),
      ('skiabot-linux-swarm-131', '131', False),
      ('skiabot-linux-swarm-132', '132', False),
      ('skiabot-linux-swarm-133', '133', False),
      ('skiabot-linux-swarm-134', '134', False),
      ('skiabot-linux-swarm-135', '135', False),
      #('skiabot-linux-swarm-136', '136', False),
      #('skiabot-linux-swarm-137', '137', False),
      #('skiabot-linux-swarm-138', '138', False),
      #('skiabot-linux-swarm-139', '139', False),
      #('skiabot-linux-swarm-140', '140', False),
      #('skiabot-linux-swarm-141', '141', False),
      #('skiabot-linux-swarm-142', '142', False),
      #('skiabot-linux-swarm-143', '143', False),
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': CHROMECOMPUTE_BUILDBOT_PATH,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

  'skia-vm-003': {
    'slaves': [
      ('skiabot-linux-swarm-096', '96', False),
      ('skiabot-linux-swarm-097', '97', False),
      ('skiabot-linux-swarm-098', '98', False),
      ('skiabot-linux-swarm-099', '99', False),
      ('skiabot-linux-swarm-100', '100', False),
      ('skiabot-linux-swarm-101', '101', False),
      ('skiabot-linux-swarm-102', '102', False),
      ('skiabot-linux-swarm-103', '103', False),
      ('skiabot-linux-swarm-104', '104', False),
      ('skiabot-linux-swarm-105', '105', False),
      ('skiabot-linux-swarm-106', '106', False),
      ('skiabot-linux-swarm-107', '107', False),
      ('skiabot-linux-swarm-108', '108', False),
      ('skiabot-linux-swarm-109', '109', False),
      ('skiabot-linux-swarm-110', '110', False),
      ('skiabot-linux-swarm-111', '111', False),
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': CHROMECOMPUTE_BUILDBOT_PATH,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

  'skia-vm-004': {
    'slaves': [
      ('skiabot-linux-swarm-064', '64', False),
      ('skiabot-linux-swarm-065', '65', False),
      ('skiabot-linux-swarm-066', '66', False),
      ('skiabot-linux-swarm-067', '67', False),
      ('skiabot-linux-swarm-068', '68', False),
      ('skiabot-linux-swarm-069', '69', False),
      ('skiabot-linux-swarm-070', '70', False),
      ('skiabot-linux-swarm-071', '71', False),
      ('skiabot-linux-swarm-072', '72', False),
      ('skiabot-linux-swarm-073', '73', False),
      ('skiabot-linux-swarm-074', '74', False),
      ('skiabot-linux-swarm-075', '75', False),
      ('skiabot-linux-swarm-076', '76', False),
      ('skiabot-linux-swarm-077', '77', False),
      ('skiabot-linux-swarm-078', '78', False),
      ('skiabot-linux-swarm-079', '79', False),
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': CHROMECOMPUTE_BUILDBOT_PATH,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

  'skia-vm-005': {
    'slaves': [
      ('skiabot-linux-swarm-048', '48', False),
      ('skiabot-linux-swarm-049', '49', False),
      ('skiabot-linux-swarm-050', '50', False),
      ('skiabot-linux-swarm-051', '51', False),
      ('skiabot-linux-swarm-052', '52', False),
      ('skiabot-linux-swarm-053', '53', False),
      ('skiabot-linux-swarm-054', '54', False),
      ('skiabot-linux-swarm-055', '55', False),
      ('skiabot-linux-swarm-056', '56', False),
      #('skiabot-linux-swarm-057', '57', False),
      ('skiabot-linux-swarm-058', '58', False),
      ('skiabot-linux-swarm-059', '59', False),
      ('skiabot-linux-swarm-060', '60', False),
      ('skiabot-linux-swarm-061', '61', False),
      ('skiabot-linux-swarm-062', '62', False),
      ('skiabot-linux-swarm-063', '63', False),
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': CHROMECOMPUTE_BUILDBOT_PATH,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

  'skia-vm-006': {
    'slaves': [
      ('skiabot-linux-swarm-032', '32', False),
      ('skiabot-linux-swarm-033', '33', False),
      ('skiabot-linux-swarm-034', '34', False),
      ('skiabot-linux-swarm-035', '35', False),
      ('skiabot-linux-swarm-036', '36', False),
      ('skiabot-linux-swarm-037', '37', False),
      ('skiabot-linux-swarm-038', '38', False),
      ('skiabot-linux-swarm-039', '39', False),
      ('skiabot-linux-swarm-040', '40', False),
      ('skiabot-linux-swarm-041', '41', False),
      ('skiabot-linux-swarm-042', '42', False),
      ('skiabot-linux-swarm-043', '43', False),
      ('skiabot-linux-swarm-044', '44', False),
      ('skiabot-linux-swarm-045', '45', False),
      ('skiabot-linux-swarm-046', '46', False),
      ('skiabot-linux-swarm-047', '47', False),
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': CHROMECOMPUTE_BUILDBOT_PATH,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

  'skia-vm-007': {
    'slaves': [
      ('skiabot-linux-swarm-016', '16', False),
      ('skiabot-linux-swarm-017', '17', False),
      ('skiabot-linux-swarm-018', '18', False),
      ('skiabot-linux-swarm-019', '19', False),
      ('skiabot-linux-swarm-020', '20', False),
      ('skiabot-linux-swarm-021', '21', False),
      ('skiabot-linux-swarm-022', '22', False),
      ('skiabot-linux-swarm-023', '23', False),
      ('skiabot-linux-swarm-024', '24', False),
      ('skiabot-linux-swarm-025', '25', False),
      ('skiabot-linux-swarm-026', '26', False),
      ('skiabot-linux-swarm-027', '27', False),
      ('skiabot-linux-swarm-028', '28', False),
      ('skiabot-linux-swarm-029', '29', False),
      #('skiabot-linux-swarm-030', '30', False),
      ('skiabot-linux-swarm-031', '31', False),
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': CHROMECOMPUTE_BUILDBOT_PATH,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

  'skia-vm-008': {
    'slaves': [
      ('skiabot-linux-swarm-000', '0', False),
      ('skiabot-linux-swarm-001', '1', False),
      ('skiabot-linux-swarm-002', '2', False),
      ('skiabot-linux-swarm-003', '3', False),
      ('skiabot-linux-swarm-004', '4', False),
      ('skiabot-linux-swarm-005', '5', False),
      ('skiabot-linux-swarm-006', '6', False),
      ('skiabot-linux-swarm-007', '7', False),
      ('skiabot-linux-swarm-008', '8', False),
      ('skiabot-linux-swarm-009', '9', False),
      ('skiabot-linux-swarm-010', '10', False),
      ('skiabot-linux-swarm-011', '11', False),
      ('skiabot-linux-swarm-012', '12', False),
      ('skiabot-linux-swarm-013', '13', False),
      ('skiabot-linux-swarm-014', '14', False),
      ('skiabot-linux-swarm-015', '15', False),
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': CHROMECOMPUTE_BUILDBOT_PATH,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

  'skia-vm-009': {
    'slaves': [
      ('skiabot-linux-swarm-080', '80', False),
      ('skiabot-linux-swarm-081', '81', False),
      ('skiabot-linux-swarm-082', '82', False),
      ('skiabot-linux-swarm-083', '83', False),
      ('skiabot-linux-swarm-084', '84', False),
      ('skiabot-linux-swarm-085', '85', False),
      ('skiabot-linux-swarm-086', '86', False),
      ('skiabot-linux-swarm-087', '87', False),
      ('skiabot-linux-swarm-088', '88', False),
      ('skiabot-linux-swarm-089', '89', False),
      ('skiabot-linux-swarm-090', '90', False),
      ('skiabot-linux-swarm-091', '91', False),
      ('skiabot-linux-swarm-092', '92', False),
      ('skiabot-linux-swarm-093', '93', False),
      ('skiabot-linux-swarm-094', '94', False),
      ('skiabot-linux-swarm-095', '95', False),
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': CHROMECOMPUTE_BUILDBOT_PATH,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

  'skia-vm-013': {
    'slaves': [
      ('skiabot-ct-dm-000', '0', False),
      ('skiabot-ct-dm-001', '1', False),
      ('skiabot-ct-dm-003', '2', False),
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': CHROMECOMPUTE_BUILDBOT_PATH,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

  'skia-vm-014': {
    'slaves': [
      ('skia-android-canary', '0', True),
      ('skia-android-build-000', '1', True),
      ('skia-android-build-001', '2', True),
      ('skia-android-build-002', '3', True),
      ('skia-android-build-003', '4', True),
      ('skia-android-build-004', '5', True),
      ('skia-android-build-005', '6', True),
      ('skia-android-build-006', '7', True),
      ('skia-android-build-007', '8', True),
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': CHROMECOMPUTE_BUILDBOT_PATH,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

  'skia-vm-015': {
    'slaves': [
      ('skiabot-ct-dm-002', '3', False),
      ('skiabot-ct-dm-004', '4', False),
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': CHROMECOMPUTE_BUILDBOT_PATH,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

  'skia-vm-024': {
    'slaves': [
    ],
    'copies': CHROMEBUILD_COPIES,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': CHROMECOMPUTE_BUILDBOT_PATH,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

################################# Mac Machines #################################

############################### Windows Machines ###############################

############################ Machines in Chrome Golo ###########################

  'slave11-c3': {
    'slaves': [
      ('slave11-c3', '0', False),
    ],
    'copies': None,
    'ip': NO_IP_ADDR,
    'kvm_switch': NO_KVM_SWITCH,
    'kvm_num': NO_KVM_NUM,
    'path_to_buildbot': None,
    'launch_script': LAUNCH_SCRIPT_UNIX,
  },

}


# Class which holds configuration data describing a build slave host.
SlaveHostConfig = collections.namedtuple('SlaveHostConfig',
                                         ('hostname, slaves, copies,'
                                          ' ip, kvm_switch, kvm_num,'
                                          ' path_to_buildbot,'
                                          ' launch_script'))


SLAVE_HOSTS = {}
for (_hostname, _config) in _slave_host_dicts.iteritems():
  SLAVE_HOSTS[_hostname] = SlaveHostConfig(hostname=_hostname,
                                           **_config)


def default_slave_host_config(hostname):
  """Return a default configuration for the given hostname.

  Assumes that the slave host is the machine on which this function is called.

  Args:
      hostname: string; name of the build slave host.
  Returns:
      SlaveHostConfig instance with configuration for this machine.
  """
  path_to_buildbot = os.path.join(os.path.dirname(__file__), os.pardir)
  path_to_buildbot = os.path.abspath(path_to_buildbot).split(os.path.sep)
  launch_script = LAUNCH_SCRIPT_WIN if os.name == 'nt' else LAUNCH_SCRIPT_UNIX
  return SlaveHostConfig(
    hostname=hostname,
    slaves=[(hostname, '0', True)],
    copies=CHROMEBUILD_COPIES,
    ip=None,
    kvm_switch=None,
    kvm_num=None,
    path_to_buildbot=path_to_buildbot,
    launch_script=launch_script,
  )


def get_slave_host_config(hostname):
  """Helper function for retrieving slave host configuration information.

  If the given hostname is unknown, returns a default config.

  Args:
      hostname: string; the hostname of the slave host machine.
  Returns:
      SlaveHostConfig instance representing the given host.
  """
  return SLAVE_HOSTS.get(hostname, default_slave_host_config(hostname))


if __name__ == '__main__':
  print json.dumps(_slave_host_dicts)
