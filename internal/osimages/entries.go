package osimages

import "regexp"

type Entry struct {
	Key, Description, OSFlavor, OSVersion string
	Dirs                                  []string
	ChecksumName                          string
	ChecksumFile, File                    *regexp.Regexp
	Algorithm, Format, CPU                string
}

func Lookup(entries []Entry, key string) (Entry, bool) {
	for _, e := range entries {
		if e.Key == key {
			return e, true
		}
	}
	return Entry{}, false
}

func debian(ver, codename string) Entry {
	return Entry{
		Key: "debian-" + ver, Description: "Debian " + ver + " generic cloud image", OSFlavor: "debian", OSVersion: ver,
		Dirs:         []string{"https://cloud.debian.org/images/cloud/" + codename + "/latest/"},
		ChecksumName: "SHA512SUMS",
		File:         regexp.MustCompile(`^debian-` + ver + `-genericcloud-amd64\.qcow2$`),
		Algorithm:    "sha512", Format: "qcow2",
	}
}

func ubuntu(ver string) Entry {
	return Entry{
		Key: "ubuntu-" + ver, Description: "Ubuntu " + ver + " server cloud image", OSFlavor: "ubuntu", OSVersion: ver,
		Dirs:         []string{"https://cloud-images.ubuntu.com/releases/" + ver + "/release/"},
		ChecksumName: "SHA256SUMS",
		File:         regexp.MustCompile(`^ubuntu-` + regexp.QuoteMeta(ver) + `-server-cloudimg-amd64\.img$`),
		Algorithm:    "sha256", Format: "qcow2",
	}
}

func rocky(ver, cpu string) Entry {
	return Entry{
		Key: "rocky-" + ver, Description: "Rocky Linux " + ver + " generic cloud image", OSFlavor: "rocky", OSVersion: ver,
		Dirs:         []string{"https://dl.rockylinux.org/pub/rocky/" + ver + "/images/x86_64/"},
		ChecksumName: "CHECKSUM",
		File:         regexp.MustCompile(`^Rocky-` + ver + `-GenericCloud-Base[.-].*\.x86_64\.qcow2$`),
		Algorithm:    "sha256", Format: "qcow2", CPU: cpu,
	}
}

func alma(ver, cpu string) Entry {
	return Entry{
		Key: "alma-" + ver, Description: "AlmaLinux " + ver + " generic cloud image", OSFlavor: "alma", OSVersion: ver,
		Dirs:         []string{"https://repo.almalinux.org/almalinux/" + ver + "/cloud/x86_64/images/"},
		ChecksumName: "CHECKSUM",
		File:         regexp.MustCompile(`^AlmaLinux-` + ver + `-GenericCloud-(latest|[0-9][0-9.]*-[0-9]+(\.[0-9]+)?)\.x86_64\.qcow2$`),
		Algorithm:    "sha256", Format: "qcow2", CPU: cpu,
	}
}

func fedora(ver string) Entry {
	rel := "/linux/releases/" + ver + "/Cloud/x86_64/images/"
	return Entry{
		Key: "fedora-" + ver, Description: "Fedora " + ver + " cloud base image", OSFlavor: "fedora", OSVersion: ver,
		Dirs: []string{
			"https://download.fedoraproject.org/pub/fedora" + rel,
			"https://download.fedoraproject.org/pub/archive/fedora" + rel,
		},
		ChecksumFile: regexp.MustCompile(`^Fedora-Cloud-` + ver + `-[0-9.]+-x86_64-CHECKSUM$`),
		File:         regexp.MustCompile(`^Fedora-Cloud-Base-Generic-` + ver + `-[0-9.]+\.x86_64\.qcow2$`),
		Algorithm:    "sha256", Format: "qcow2",
	}
}

// Entries returns the compiled-in catalog in display order.
func Entries() []Entry {
	return []Entry{
		debian("12", "bookworm"),
		debian("13", "trixie"),
		ubuntu("24.04"),
		ubuntu("26.04"),
		rocky("9", ""),
		rocky("10", "x86-64-v3"),
		alma("9", ""),
		alma("10", "x86-64-v3"),
		fedora("45"),
	}
}
