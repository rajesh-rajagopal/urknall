package zwo

import (
	"crypto/sha256"
	"fmt"
	"github.com/dynport/gologger"
	"github.com/dynport/gossh"
	"github.com/dynport/zwo/host"
	"path"
	"runtime/debug"
	"strings"
)

type sshClient struct {
	client *gossh.Client
	host   *host.Host
}

func (sc *sshClient) Provision(packages ...Compiler) (e error) {
	logger.PushPrefix(sc.host.GetIPAddress())
	defer logger.PopPrefix()

	if packages == nil || len(packages) == 0 {
		e := fmt.Errorf("compilables must be given")
		logger.Errorf(e.Error())
		return e
	}

	runLists, e := sc.precompileRunlists(packages...)
	if e != nil {
		return e
	}

	return sc.provisionRunlists(runLists)
}

func (sc *sshClient) precompileRunlists(packages ...Compiler) (runLists []*Runlist, e error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			e, ok = r.(error)
			if !ok {
				e = fmt.Errorf("failed to precompile package: %v", r)
			}
			logger.Info(e.Error())
			logger.Debug(string(debug.Stack()))
		}
	}()

	runLists = make([]*Runlist, 0, len(packages))

	for _, pkg := range packages { // Precompile runlists.
		pkgName := getPackageName(pkg)

		rl := &Runlist{host: sc.host}
		rl.setConfig(pkg)
		rl.setName(pkgName)
		pkg.Compile(rl)

		runLists = append(runLists, rl)
	}

	return runLists, nil
}

func (sc *sshClient) provisionRunlists(runLists []*Runlist) (e error) {
	for i := range runLists {
		rl := runLists[i]
		logger.PushPrefix(padToFixedLength(rl.getName(), 15))
		if e = sc.provision(runLists[i]); e != nil {
			logger.Errorf("failed to provision: %s", e.Error())
			return e
		}
		logger.PopPrefix()
	}
	return nil
}

func (sc *sshClient) provision(rl *Runlist) (e error) {
	tasks := buildTasksForRunlist(rl)

	checksumDir := fmt.Sprintf("/var/cache/zwo/tree/%s", rl.getName())

	checksumHash, e := sc.buildChecksumHash(checksumDir)
	if e != nil {
		return fmt.Errorf("failed to build checksum hash: %s", e.Error())
	}

	for i := range tasks {
		task := tasks[i]
		logMsg := task.action.Logging()
		if _, found := checksumHash[task.checksum]; found { // Task is cached.
			logger.Infof("[%s] [%.8s] %s", gologger.Colorize(33, "CACHED"), task.checksum, logMsg)
			delete(checksumHash, task.checksum) // Delete checksums of cached tasks from hash.
			continue
		}

		if len(checksumHash) > 0 { // All remaining checksums are invalid, as something changed.
			if e = sc.cleanUpRemainingCachedEntries(checksumDir, checksumHash); e != nil {
				return e
			}
			checksumHash = make(map[string]struct{})
		}

		logger.Infof("[%s  ] [%.8s] %s", gologger.Colorize(34, "EXEC"), task.checksum, logMsg)
		if e = sc.runTask(task, checksumDir); e != nil {
			return e
		}
	}

	return nil
}

func (sc *sshClient) runTask(task *taskData, checksumDir string) (e error) {
	checksumFile := fmt.Sprintf("%s/%s", checksumDir, task.checksum)

	rsp, e := sc.client.Execute(task.action.Shell())

	// Write the checksum file (containing information on the command run).
	sc.writeChecksumFile(checksumFile, e != nil, task.action.Logging(), rsp)

	return e
}

func (sc *sshClient) executeCommand(cmdRaw string) *gossh.Result {
	c := &commandAction{cmd: cmdRaw, host: sc.host}
	result, e := sc.client.Execute(c.Shell())
	if e != nil {
		stderr := ""
		if result != nil {
			stderr = strings.TrimSpace(result.Stderr())
		}
		panic(fmt.Errorf("internal error: %s (%s)", e.Error(), stderr))
	}
	return result
}

func (sc *sshClient) buildChecksumHash(checksumDir string) (checksumMap map[string]struct{}, e error) {
	// Make sure the directory exists.
	sc.executeCommand(fmt.Sprintf("mkdir -p %s", checksumDir))

	checksums := []string{}
	rsp := sc.executeCommand(fmt.Sprintf("ls %s/*.done | xargs", checksumDir))
	for _, checksumFile := range strings.Fields(rsp.Stdout()) {
		checksum := strings.TrimSuffix(path.Base(checksumFile), ".done")
		checksums = append(checksums, checksum)
	}

	checksumMap = make(map[string]struct{})
	for i := range checksums {
		if len(checksums[i]) != 64 {
			return nil, fmt.Errorf("invalid checksum '%s' found in '%s'", checksums[i], checksumDir)
		}
		checksumMap[checksums[i]] = struct{}{}
	}
	return checksumMap, nil
}

func (sc *sshClient) cleanUpRemainingCachedEntries(checksumDir string, checksumHash map[string]struct{}) (e error) {
	invalidCacheEntries := make([]string, 0, len(checksumHash))
	for k, _ := range checksumHash {
		invalidCacheEntries = append(invalidCacheEntries, fmt.Sprintf("%s.done", k))
	}
	cmd := fmt.Sprintf("cd %s && rm -f *.failed %s", checksumDir, strings.Join(invalidCacheEntries, " "))
	logger.Debug(cmd)
	sc.executeCommand(cmd)
	return nil
}

func (sc *sshClient) writeChecksumFile(checksumFile string, failed bool, logMsg string, response *gossh.Result) {
	content := []string{}
	content = append(content, fmt.Sprintf("Command: %s", logMsg))
	content = append(content, "Wrote to STDOUT: #################")
	content = append(content, response.Stdout())
	content = append(content, "Wrote to STDERR: #################")
	content = append(content, response.Stderr())

	if failed {
		checksumFile += ".failed"
	} else {
		checksumFile += ".done"
	}

	c := &fileAction{
		path:    checksumFile,
		content: strings.Join(content, "\n"),
		host:    sc.host}

	if _, e := sc.client.Execute(c.Shell()); e != nil {
		panic(fmt.Sprintf("failed to write checksum file: %s", e.Error()))
	}
}

type taskData struct {
	action   action // The command to be executed.
	checksum string // The checksum of the command.
}

func buildTasksForRunlist(rl *Runlist) (tasks []*taskData) {
	tasks = make([]*taskData, 0, len(rl.actions))

	cmdHash := sha256.New()
	for i := range rl.actions {
		rawCmd := rl.actions[i].Shell()
		cmdHash.Write([]byte(rawCmd))

		task := &taskData{action: rl.actions[i], checksum: fmt.Sprintf("%x", cmdHash.Sum(nil))}
		tasks = append(tasks, task)
	}
	return tasks
}