//go:build linux

package procguard

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/link"
	"github.com/kombifyio/SpeechKit/internal/dataprofilev1"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"golang.org/x/sys/unix"
)

// No daemon or delegated writable ancestor is trusted. This launcher supports
// root-owned cgroup v2 only; native images map to host nobody in a private user
// namespace with no mapped root, supplementary groups, effective capabilities,
// new privileges, inherited sockets or cgroup FDs.
const nativeUID = 65534

func prepareChildRead(path string) error {
	if err := confinementAvailable(); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	mode := os.FileMode(0400)
	if st.IsDir() {
		mode = 0500
	} else if !st.Mode().IsRegular() {
		return errors.New("native identity must be a regular file or directory")
	}
	if err := f.Chown(nativeUID, nativeUID); err != nil {
		return err
	}
	return f.Chmod(mode)
}

type confinedGroup struct {
	dir      string
	fd       *os.File
	loopback bool
	pid      int
}

var groupsMu sync.Mutex
var confinedGroups = map[*confinedGroup]struct{}{}

func adoptConfined(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return errors.New("native process has not started")
	}
	groupsMu.Lock()
	defer groupsMu.Unlock()
	for g := range confinedGroups {
		if g.pid != cmd.Process.Pid {
			continue
		}
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", g.pid))
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "0::"+strings.TrimPrefix(g.dir, "/sys/fs/cgroup")+"\n") {
			return nil
		}
	}
	return errors.New("native process is not in an armed owned cgroup")
}

func shutdownConfined() {
	groupsMu.Lock()
	defer groupsMu.Unlock()
	for g := range confinedGroups {
		_ = g.kill()
	}
}

func init() { netsec.RegisterPolicyGuard(tightenNativePolicy) }

func confinementAvailable() error {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return fmt.Errorf("%w: native Linux architecture %s is not admitted", ErrNetworkConfinementRequired, runtime.GOARCH)
	}
	if os.Geteuid() != 0 || os.Getuid() != 0 {
		return fmt.Errorf("%w: Linux requires a privileged parent and an unprivileged native child", ErrNetworkConfinementRequired)
	}
	var fs unix.Statfs_t
	if err := unix.Statfs("/sys/fs/cgroup", &fs); err != nil || fs.Type != unix.CGROUP2_SUPER_MAGIC {
		return fmt.Errorf("%w: writable cgroup v2 is required", ErrNetworkConfinementRequired)
	}
	return nil
}

func nativeLoopback(p netsec.Policy) bool {
	return p.Validation != nil && !p.Validation.AllowPrivate
}

func tightenNativePolicy(p netsec.Policy) {
	if !netsec.FullLocal() || !nativeLoopback(p) {
		return
	}
	groupsMu.Lock()
	defer groupsMu.Unlock()
	for g := range confinedGroups {
		if g.loopback {
			continue
		}
		if err := g.arm(true); err != nil {
			// There can be no successful scope change with a surviving wider
			// child. Kill the entire owned subtree, never detach its old filter.
			if killErr := g.kill(); killErr != nil {
				panic(fmt.Errorf("native scope tightening failed: %v; subtree kill: %w", err, killErr))
			}
		}
	}
}

func startConfined(cmd *exec.Cmd) error {
	if err := confinementAvailable(); err != nil {
		return err
	}
	if cmd == nil || cmd.Process != nil || cmd.Err != nil {
		return fmt.Errorf("%w: invalid command", ErrNetworkConfinementRequired)
	}
	if len(cmd.ExtraFiles) != 0 || cmd.SysProcAttr != nil {
		return fmt.Errorf("%w: inherited descriptors or custom process credentials are not admitted", ErrNetworkConfinementRequired)
	}
	for _, stream := range []any{cmd.Stdin, cmd.Stdout, cmd.Stderr} {
		if f, ok := stream.(*os.File); ok {
			var st unix.Stat_t
			if err := unix.Fstat(int(f.Fd()), &st); err != nil || st.Mode&unix.S_IFMT == unix.S_IFSOCK {
				return fmt.Errorf("%w: inherited socket stream", ErrNetworkConfinementRequired)
			}
		}
	}
	return netsec.WithPolicy(func(p netsec.Policy) error {
		g, err := newConfinedGroup()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrNetworkConfinementRequired, err)
		}
		if err = g.arm(nativeLoopback(p)); err != nil {
			g.remove()
			return fmt.Errorf("%w: %v", ErrNetworkConfinementRequired, err)
		}
		result := make(chan error, 1)
		go func() {
			// A locked thread is discarded when this goroutine exits. NNP,
			// securebits and bounding-set changes never contaminate a Go worker.
			// Keep it alive until pidfd signals exit: PDEATHSIG names this thread.
			runtime.LockOSThread()
			if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
				result <- err
				return
			}
			if err := unix.Prctl(unix.PR_SET_SECUREBITS, 0, 0, 0, 0); err != nil {
				result <- err
				return
			}
			for cap := 0; ; cap++ {
				err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(cap), 0, 0, 0)
				if errors.Is(err, unix.EINVAL) {
					break
				}
				if err != nil {
					result <- err
					return
				}
			}
			// A private user namespace also prevents a same-host-UID process
			// outside this leaf from being used via ptrace or /proc/PID/mem to
			// execute outside the filter. Drop supplementary groups on this
			// disposable thread before creating a setgroups-disabled namespace.
			if _, _, errno := unix.RawSyscall(unix.SYS_SETGROUPS, 0, 0, 0); errno != 0 {
				result <- errno
				return
			}
			if err := restrictDescriptorImport(); err != nil {
				result <- err
				return
			}
			pidfd := -1
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Credential:                 &syscall.Credential{Uid: 1, Gid: 1, NoSetGroups: true},
				Cloneflags:                 unix.CLONE_NEWUSER,
				UidMappings:                []syscall.SysProcIDMap{{ContainerID: 1, HostID: nativeUID, Size: 1}},
				GidMappings:                []syscall.SysProcIDMap{{ContainerID: 1, HostID: nativeUID, Size: 1}},
				GidMappingsEnableSetgroups: false,
				UseCgroupFD:                true, CgroupFD: int(g.fd.Fd()), PidFD: &pidfd,
				Pdeathsig: syscall.SIGKILL, Setpgid: true,
			}
			if err := cmd.Start(); err != nil {
				result <- err
				return
			}
			g.pid = cmd.Process.Pid
			groupsMu.Lock()
			confinedGroups[g] = struct{}{}
			groupsMu.Unlock()
			slog.Info("native confinement armed", "cgroup", g.dir, "pid", cmd.Process.Pid, "uid", nativeUID, "loopback_only", g.loopback)
			result <- nil
			if pidfd >= 0 {
				defer unix.Close(pidfd)
				poll := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
				for {
					_, err := unix.Poll(poll, -1)
					if !errors.Is(err, unix.EINTR) {
						break
					}
				}
			}
			groupsMu.Lock()
			delete(confinedGroups, g)
			// Grandchildren may have reparented or changed session. cgroup.kill
			// reaches them; the attachment remains until the subtree is empty.
			_ = g.kill()
			g.remove()
			groupsMu.Unlock()
		}()
		if err = <-result; err != nil {
			g.remove()
			return fmt.Errorf("%w: native spawn: %v", ErrNetworkConfinementRequired, err)
		}
		return nil
	})
}

// A socket received from outside the leaf would retain its creator's socket
// cgroup. Prevent native code and descendants importing such a descriptor,
// including through io_uring, rather than trusting the sender or its UID.
// Ordinary read/recvfrom and the production HTTP listener remain available.
func restrictDescriptorImport() error {
	arch := uint32(unix.AUDIT_ARCH_X86_64)
	if runtime.GOARCH == "arm64" {
		arch = unix.AUDIT_ARCH_AARCH64
	}
	deny := uint32(unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM))
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: arch, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: deny},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		// Refuse x32/compat calls; their syscall numbers have other meanings.
		{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, K: 0x40000000, Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: deny},
	}
	for _, nr := range []uint32{unix.SYS_RECVMSG, unix.SYS_RECVMMSG, unix.SYS_PIDFD_GETFD, unix.SYS_IO_URING_SETUP, unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV} {
		filter = append(filter, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: nr, Jf: 1}, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: deny})
	}
	// IP egress BPF does not see VSOCK and other non-IP transports. Native
	// speech needs only local Unix IPC and the guarded IPv4/IPv6 families.
	filter = append(filter,
		unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_SOCKET, Jt: 1},
		unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_SOCKETPAIR, Jf: 5},
		unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16}, // seccomp_data.args[0]
		unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.AF_UNIX, Jt: 3},
		unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.AF_INET, Jt: 2},
		unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.AF_INET6, Jt: 1},
		unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: deny},
		unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	)
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&program)), 0, 0)
	runtime.KeepAlive(filter)
	return err
}

func newConfinedGroup() (*confinedGroup, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return nil, err
	}
	var parent string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "0::/") {
			parent = filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(line, "0::"))
		}
	}
	if parent == "" {
		return nil, errors.New("no unified cgroup membership")
	}
	// Reject namespace-relative mounts and every writable/delegated ancestor.
	// An unprivileged child must be unable to write a destination cgroup.procs
	// or the common ancestor, even if it creates another user namespace.
	for dir := parent; ; dir = filepath.Dir(dir) {
		for _, path := range []string{dir, filepath.Join(dir, "cgroup.procs")} {
			var st unix.Stat_t
			if err := unix.Lstat(path, &st); err != nil {
				return nil, err
			}
			if st.Uid != 0 || st.Mode&0022 != 0 || st.Mode&unix.S_IFMT == unix.S_IFLNK {
				return nil, fmt.Errorf("unsafe cgroup ancestor %s", path)
			}
		}
		if dir == "/sys/fs/cgroup" {
			break
		}
		if !strings.HasPrefix(dir, "/sys/fs/cgroup/") {
			return nil, errors.New("cgroup outside unified mount")
		}
	}
	// A random, exclusively-created leaf is the only host cgroup we mutate.
	dir, err := os.MkdirTemp(parent, "speechkit-native-")
	if err != nil {
		return nil, err
	}
	fd, err := os.Open(dir)
	if err != nil {
		_ = os.Remove(dir)
		return nil, err
	}
	return &confinedGroup{dir: dir, fd: fd}, nil
}

func (g *confinedGroup) arm(loopback bool) error {
	prog, err := nativeEgressProgram(loopback)
	if err != nil {
		return err
	}
	info, err := prog.Info()
	if err != nil {
		prog.Close()
		return err
	}
	id, ok := info.ID()
	if !ok {
		prog.Close()
		return errors.New("kernel program identity unavailable")
	}
	err = link.RawAttachProgram(link.RawAttachProgramOptions{Target: int(g.fd.Fd()), Program: prog, Attach: ebpf.AttachCGroupInetEgress, Flags: unix.BPF_F_ALLOW_MULTI})
	// BPF_PROG_ATTACH holds a kernel reference independently of these FDs.
	prog.Close()
	if err != nil {
		return err
	}
	q, err := link.QueryPrograms(link.QueryOptions{Target: int(g.fd.Fd()), Attach: ebpf.AttachCGroupInetEgress})
	if err != nil {
		return err
	}
	found := false
	for _, entry := range q.Programs {
		if entry.ID == id {
			found = true
		}
	}
	if !found {
		return errors.New("kernel did not retain the egress program")
	}
	readback, err := ebpf.NewProgramFromID(id)
	if err != nil {
		return err
	}
	defer readback.Close()
	if err := qualifyNativeProgram(readback, loopback); err != nil {
		return err
	}
	g.loopback = loopback
	slog.Info("native egress kernel readback", "cgroup", g.dir, "program_id", id, "tag", info.Tag, "loopback_only", loopback)
	return nil
}

func (g *confinedGroup) kill() error {
	if err := os.WriteFile(filepath.Join(g.dir, "cgroup.kill"), []byte("1"), 0600); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join(g.dir, "cgroup.events"))
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "populated 0") {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("native cgroup is still populated")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (g *confinedGroup) remove() {
	_ = g.fd.Close()
	// Never detach. If anything remains alive, rmdir fails and its kernel
	// policy remains attached, including after an abrupt parent death.
	_ = os.Remove(g.dir)
}

// Compile the projected canonical policy to a packet filter, exclusions first.
// There is no second address classifier or policy table in this package.
func nativeEgressProgram(loopback bool) (*ebpf.Program, error) {
	prefixes, excluded := dataprofile.LocalDestinationPolicy()
	// LocalAddr normalizes IPv4-mapped IPv6. Preserve that same decision for
	// a packet carrying a mapped destination; ordinary mapped sockets emit v4.
	for _, prefix := range append([]netip.Prefix(nil), prefixes...) {
		if prefix.Addr().Is4() {
			prefixes = append(prefixes, netip.PrefixFrom(netip.AddrFrom16(prefix.Addr().As16()), 96+prefix.Bits()))
		}
	}
	for _, addr := range append([]netip.Addr(nil), excluded...) {
		if addr.Is4() {
			excluded = append(excluded, netip.AddrFrom16(addr.As16()))
		}
	}
	ins := asm.Instructions{asm.LoadMem(asm.R7, asm.R1, 76, asm.Word), asm.LoadMem(asm.R8, asm.R1, 80, asm.Word), asm.Mov.Reg(asm.R9, asm.R7), asm.Add.Imm(asm.R9, 1), asm.JGT.Reg(asm.R9, asm.R8, "deny"), asm.LoadMem(asm.R0, asm.R7, 0, asm.Byte), asm.RSh.Imm(asm.R0, 4), asm.JEq.Imm(asm.R0, 4, "v4"), asm.JEq.Imm(asm.R0, 6, "v6"), asm.Ja.Label("deny")}
	for _, family := range []int{4, 6} {
		length := int32(20)
		if family == 6 {
			length = 40
		}
		ins = append(ins, asm.Mov.Reg(asm.R9, asm.R7).WithSymbol(fmt.Sprintf("v%d", family)), asm.Add.Imm(asm.R9, length), asm.JGT.Reg(asm.R9, asm.R8, "deny"))
		var rules []struct {
			prefix netip.Prefix
			target string
		}
		for _, addr := range excluded {
			if addr.Is4() == (family == 4) {
				rules = append(rules, struct {
					prefix netip.Prefix
					target string
				}{netip.PrefixFrom(addr, addr.BitLen()), "deny"})
			}
		}
		for _, prefix := range prefixes {
			if prefix.Addr().Is4() == (family == 4) && (!loopback || prefix.Addr().Unmap().IsLoopback()) {
				rules = append(rules, struct {
					prefix netip.Prefix
					target string
				}{prefix, "allow"})
			}
		}
		for i, rule := range rules {
			label := fmt.Sprintf("next%d_%d", family, i)
			b := rule.prefix.Masked().Addr().AsSlice()
			for offset, bits := 0, rule.prefix.Bits(); bits > 0; offset, bits = offset+4, bits-32 {
				n := min(bits, 32)
				mask := ^uint32(0) << (32 - n)
				base := 16
				if family == 6 {
					base = 24
				}
				ins = append(ins, asm.LoadMem(asm.R0, asm.R7, int16(base+offset), asm.Word), asm.HostTo(asm.BE, asm.R0, asm.Word), asm.And.Imm(asm.R0, int32(mask)), asm.JNE.Imm32(asm.R0, int32(binary.BigEndian.Uint32(b[offset:offset+4])&mask), label))
			}
			ins = append(ins, asm.Ja.Label(rule.target), asm.Mov.Imm(asm.R0, 0).WithSymbol(label))
		}
		ins = append(ins, asm.Ja.Label("deny"))
	}
	ins = append(ins, asm.Mov.Imm(asm.R0, 1).WithSymbol("allow"), asm.Return(), asm.Mov.Imm(asm.R0, 0).WithSymbol("deny"), asm.Return())
	return ebpf.NewProgram(&ebpf.ProgramSpec{Name: "sk_native_lan", Type: ebpf.CGroupSKB, AttachType: ebpf.AttachCGroupInetEgress, License: "MIT", Instructions: ins})
}

// Execute the actual retained kernel program before releasing any native image.
// Packet probes below are an arming check, not a substitute for API qualification.
func qualifyNativeProgram(p *ebpf.Program, loopback bool) error {
	prefixes, excluded := dataprofile.LocalDestinationPolicy()
	addresses := append([]netip.Addr(nil), excluded...)
	for _, prefix := range prefixes {
		first := prefix.Masked().Addr()
		lastBytes := first.AsSlice()
		for bit := prefix.Bits(); bit < len(lastBytes)*8; bit++ {
			lastBytes[bit/8] |= 1 << (7 - bit%8)
		}
		last, _ := netip.AddrFromSlice(lastBytes)
		addresses = append(addresses, first, last)
		if a := first.Prev(); a.IsValid() {
			addresses = append(addresses, a)
		}
		if a := last.Next(); a.IsValid() {
			addresses = append(addresses, a)
		}
	}
	for _, addr := range append([]netip.Addr(nil), addresses...) {
		if addr.Is4() {
			addresses = append(addresses, netip.AddrFrom16(addr.As16()))
		}
	}
	for _, raw := range []string{"198.51.100.93", "203.0.113.93", "8.8.8.8", "2001:db8::93", "::ffff:198.51.100.93", "0.0.0.0", "::"} {
		addresses = append(addresses, netip.MustParseAddr(raw))
	}
	for _, addr := range addresses {
		packet := make([]byte, 64)
		if addr.Is4() {
			packet[0] = 0x45
			packet[9] = 17
			binary.BigEndian.PutUint16(packet[2:], 64)
			copy(packet[16:], addr.AsSlice())
		} else {
			packet[0] = 0x60
			packet[6] = 17
			binary.BigEndian.PutUint16(packet[4:], 24)
			copy(packet[24:], addr.AsSlice())
		}
		want := uint32(0)
		if dataprofile.LocalAddr(addr) && (!loopback || addr.Unmap().IsLoopback()) {
			want = 1
		}
		// BPF_PROG_TEST_RUN consumes an Ethernet frame, then presents the
		// network header to a cgroup_skb program just as the live hook does.
		frame := make([]byte, 14+len(packet))
		etherType := uint16(unix.ETH_P_IPV6)
		if addr.Is4() {
			etherType = unix.ETH_P_IP
		}
		binary.BigEndian.PutUint16(frame[12:], etherType)
		copy(frame[14:], packet)
		got, _, err := p.Test(frame)
		if err != nil {
			return fmt.Errorf("kernel packet readback: %w", err)
		}
		if got != want {
			return fmt.Errorf("kernel policy differs from canonical destination policy for %s", addr)
		}
	}
	return nil
}
