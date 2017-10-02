package inotifyinformer

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"kubevirt.io/kubevirt/pkg/logging"
	"kubevirt.io/kubevirt/pkg/virt-handler/virtwrap/api"
)

// NewFileListWatchFromClient creates a ListWatcher which watches for file
// creations, recreations, and deletions.
// It is a special ListWatcher, since it can't be used to stay completely
// in sync with the file system content. Instead it provides at-least-once
// delivery of events, where the order on an initial sync is not guaranteed.
// Specifically, create/modify events are delivered at least once, and delete
// events will be delivered exactly once.

// While for many tasks this is not good enough, it is a sufficient pattern
// to use the socket creation as a secondary resource for the VM controller
// in virt-handler

// TODO: In case Watch is never called, we could leak inotify go-routines,
// since it is not guaranteed that Stop() would ever be called. Since the
// ListWatcher is only created once at start-up that is not an issue right now

func NewFileListWatchFromClient(fileDir string) cache.ListerWatcher {

	d := &DirectoryListWatcher{
		fileDir:                  fileDir,
		backgroundWatcherStarted: false,
	}
	return d
}

type DirectoryListWatcher struct {
	lock                     sync.Mutex
	wg                       sync.WaitGroup
	fileDir                  string
	watcher                  *fsnotify.Watcher
	stopChan                 chan struct{}
	eventChan                chan watch.Event
	backgroundWatcherStarted bool
}

func splitFileNamespaceName(fullPath string) (namespace string, domain string, err error) {
	fileName := filepath.Base(fullPath)
	namespaceName := strings.Split(fileName, "_")
	if len(namespaceName) != 2 {
		return "", "", fmt.Errorf("Invalid file path: %s", fullPath)
	}

	namespace = namespaceName[0]
	domain = namespaceName[1]
	return namespace, domain, nil
}

func (d *DirectoryListWatcher) startBackground() error {
	d.lock.Lock()
	defer d.lock.Unlock()

	var err error
	if d.backgroundWatcherStarted == true {
		return nil
	}

	d.stopChan = make(chan struct{}, 1)
	d.eventChan = make(chan watch.Event, 100)

	d.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	err = d.watcher.Add(d.fileDir)
	if err != nil {
		return err
	}

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		for {
			var e watch.EventType
			var fse fsnotify.Event
			select {
			case <-d.stopChan:
				d.watcher.Close()
				return
			case event := <-d.watcher.Events:
				fse = event
				switch event.Op {
				case fsnotify.Create:
					e = watch.Added
				case fsnotify.Remove:
					e = watch.Deleted
				}
			case err := <-d.watcher.Errors:
				d.eventChan <- watch.Event{
					Type: watch.Error,
					Object: &v1.Status{
						Status: v1.StatusFailure, Message: err.Error(),
					},
				}
			}
			namespace, name, err := splitFileNamespaceName(fse.Name)
			if err != nil {
				logging.DefaultLogger().Error().Reason(err).Msg("Invalid content detected, ignoring and continuing.")
				continue
			}
			d.eventChan <- watch.Event{Type: e, Object: api.NewMinimalDomainWithNS(namespace, name)}
		}
	}()

	d.backgroundWatcherStarted = true
	return nil
}

func (d *DirectoryListWatcher) List(options v1.ListOptions) (runtime.Object, error) {

	// This starts the watch already.
	// Starting watching before the actual sync, has the advantage, that we don't
	// miss notifications about file changes.
	// It also means that we can't reliably follow file system changes, because we
	// are informed at least once about changes.
	err := d.startBackground()
	if err != nil {
		return nil, err
	}

	files, err := ioutil.ReadDir(d.fileDir)
	if err != nil {
		d.Stop()
		return nil, err
	}

	domainList := &api.DomainList{
		Items: []api.Domain{},
	}
	for _, file := range files {
		namespace, name, err := splitFileNamespaceName(file.Name())
		if err != nil {
			logging.DefaultLogger().Error().Reason(err).Msg("Invalid content detected, ignoring and continuing.")
			continue
		}
		domainList.Items = append(domainList.Items, *api.NewMinimalDomainWithNS(namespace, name))

	}
	return domainList, nil
}

func (d *DirectoryListWatcher) Watch(options v1.ListOptions) (watch.Interface, error) {
	return d, nil
}

func (d *DirectoryListWatcher) Stop() {
	d.lock.Lock()
	defer d.lock.Unlock()

	if d.backgroundWatcherStarted == false {
		return
	}
	close(d.stopChan)
	d.wg.Wait()
	d.backgroundWatcherStarted = false
}

func (d *DirectoryListWatcher) ResultChan() <-chan watch.Event {
	return d.eventChan
}
