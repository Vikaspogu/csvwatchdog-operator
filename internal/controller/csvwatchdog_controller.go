/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/go-logr/logr"
	"github.com/google/go-github/v74/github"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	botv1alpha1 "github.com/vikaspogu/csvwatchdog-operator/api/v1alpha1"
)

var log logr.Logger
var scm SCM

// CSVWatchdogReconciler reconciles a CSVWatchdog object
type CSVWatchdogReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type InstallPlanVersion struct {
	CSV       string `json:"csv"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type SCM struct {
	Username   string `json:"username"`
	RepoName   string `json:"repoName"`
	Token      string `json:"token"`
	SearchPath string `json:"searchPath"`
}

// +kubebuilder:rbac:groups=bot.vikaspogu.com,resources=csvwatchdogs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bot.vikaspogu.com,resources=csvwatchdogs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bot.vikaspogu.com,resources=csvwatchdogs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="operators.coreos.com",resources=subscriptions,verbs=get;list;watch
// +kubebuilder:rbac:groups="operators.coreos.com",resources=installplans,verbs=get;list;watch;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the CSVWatchdog object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *CSVWatchdogReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log = logf.FromContext(ctx)
	csvwatchdog := &botv1alpha1.CSVWatchdog{}
	if err := r.Get(ctx, req.NamespacedName, csvwatchdog); err == nil {
		log.Info("Reconcile CSVWatchdog", "spec", csvwatchdog.Spec)
		return r.reconcileCSVWatchDog(ctx, req, csvwatchdog)
	} else if !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	installPlan := &operatorsv1alpha1.InstallPlan{}
	if err := r.Get(ctx, req.NamespacedName, installPlan); err == nil {
		log.Info("Reconcile InstallPlan", "spec", installPlan.Spec)
		return r.reconcileInstallPlan(ctx, req, installPlan)
	} else if !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *CSVWatchdogReconciler) reconcileInstallPlan(ctx context.Context, req ctrl.Request, installPlan *operatorsv1alpha1.InstallPlan) (reconcile.Result, error) {

	if scm.RepoName == "" || scm.Username == "" || scm.Token == "" {
		log.Info("SCM variables are not defined; Skipping the installPlan...")
		return reconcile.Result{}, nil
	}

	var installPlanVersions []InstallPlanVersion
	if err := r.Get(ctx, req.NamespacedName, installPlan); err != nil {
		log.Error(err, "Cannot get install plans")
		return ctrl.Result{}, err
	} else {
		labels := installPlan.GetLabels()
		_, ok := labels["cvswatchdog/pr_open"]
		if installPlan.Spec.Approval == "Manual" && !ok {
			installPlanVersions = append(installPlanVersions, InstallPlanVersion{
				CSV:       installPlan.Spec.ClusterServiceVersionNames[0],
				Name:      installPlan.Name,
				Namespace: installPlan.Namespace,
			})
		} else if installPlan.Spec.Approval == "Manual" && ok {
			log.Info("Skipping the installPlan " + installPlan.Name)
		}
	}

	for _, installPlanVersion := range installPlanVersions {

		continueOpenPR, err := cloneRepoAndSearch(&scm, installPlanVersion.CSV)
		if err != nil {
			log.Error(err, "Cannot clone and search")
			return ctrl.Result{}, err
		}

		if continueOpenPR {
			err = openPR(&scm, installPlanVersion.CSV, ctx)
			if err != nil {
				log.Error(err, "Cannot clone and search")
				return ctrl.Result{}, err
			}
			// update installplan
			installPlan := &operatorsv1alpha1.InstallPlan{}
			if err = r.Get(ctx, types.NamespacedName{
				Name:      installPlanVersion.Name,
				Namespace: installPlanVersion.Namespace,
			}, installPlan); err != nil {
				log.Error(err, "Cannot get plan")
				return ctrl.Result{}, err
			} else {
				oldInstallPlan := installPlan.DeepCopy()
				installPlan.Labels["cvswatchdog/pr_open"] = "true"
				if err := r.Patch(ctx, installPlan, client.MergeFrom(oldInstallPlan)); err != nil {
					log.Error(err, "unable to patch labels")
					return ctrl.Result{}, err
				}
			}
		}
	}
	return reconcile.Result{}, nil
}

func (r *CSVWatchdogReconciler) reconcileCSVWatchDog(ctx context.Context, req ctrl.Request, csvwatchdog *botv1alpha1.CSVWatchdog) (reconcile.Result, error) {
	err := r.Get(ctx, req.NamespacedName, csvwatchdog)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("csvwatchdog resource not found.")
			return reconcile.Result{}, err
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get csvwatchdog")
		return reconcile.Result{}, err
	} else {
		scm.RepoName = csvwatchdog.Spec.RepositoryName
		scm.Username = csvwatchdog.Spec.RepositoryOwner
		scm.SearchPath = csvwatchdog.Spec.Path
	}

	// Check if its github secrets
	if csvwatchdog.Spec.GitHubCredentials.Name != "" {
		secret := &corev1.Secret{}
		err := r.Get(ctx, types.NamespacedName{
			Name:      csvwatchdog.Spec.GitHubCredentials.Name,
			Namespace: csvwatchdog.Spec.GitHubCredentials.Namespace,
		}, secret)
		if err != nil {
			log.Error(err, "SCM secret not found")
			return reconcile.Result{}, err
		}
		if data, found := secret.Data[csvwatchdog.Spec.GitHubCredentials.Key]; found {
			scm.Token = string(data)
		}
		meta.SetStatusCondition(&csvwatchdog.Status.Conditions, metav1.Condition{
			Type:               "Success",
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: csvwatchdog.GetGeneration(),
			Reason:             "csvwatchdog_reconciled",
			Status:             metav1.ConditionTrue})

		if err := r.Status().Update(ctx, csvwatchdog); err != nil {
			log.Error(err, "Failed to update CVSWatchDog status")
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

func openPR(scm *SCM, csvName string, ctx context.Context) error {
	githubClient := github.NewClient(nil).WithAuthToken(scm.Token)
	newPR := &github.NewPullRequest{
		Title:               github.Ptr("fix:(csv update): update csv to " + csvName),
		Head:                github.Ptr("csvwatchdog/" + csvName),
		Base:                github.Ptr("main"),
		Body:                github.Ptr("fix:(csv update): update csv to " + csvName),
		MaintainerCanModify: github.Ptr(true),
	}
	_, resp, err := githubClient.PullRequests.Create(ctx, scm.Username, scm.RepoName, newPR)
	if err != nil {
		if resp != nil && resp.StatusCode == 422 {
			log.Info("a pull request for this branch already exists")
			return nil
		}
		log.Error(err, "Cannot create PR")
		return err
	}
	return nil
}

func cloneRepoAndSearch(scm *SCM, csvName string) (bool, error) {
	// 1) Repo & auth config
	var repoBuilder strings.Builder
	repoBuilder.WriteString("https://github.com")
	repoBuilder.WriteString("/")
	repoBuilder.WriteString(scm.Username)
	repoBuilder.WriteString("/")
	repoBuilder.WriteString(scm.RepoName)
	repoBuilder.WriteString(".git")
	repoURL := repoBuilder.String()
	branch := "csvwatchdog/" + csvName
	auth := &http.BasicAuth{
		Username: scm.Username,
		Password: scm.Token,
	}

	// 2) In-memory storage & filesystem
	storer := memory.NewStorage()
	fs := memfs.New()

	// 3) Clone into memory
	repo, err := git.Clone(storer, fs, &git.CloneOptions{
		URL:           repoURL,
		SingleBranch:  true,
		ReferenceName: plumbing.Main,
		Auth:          auth,
	})
	if err != nil {
		log.Error(err, "Cannot clone")
		return false, err
	}

	wt, _ := repo.Worktree()
	if err := wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
		Create: true,
		Keep:   false,
	}); err != nil {
		log.Error(err, "Cannot set branch")
		return false, err
	}

	err = searchFS(fs, "/"+scm.SearchPath, csvName)
	if err != nil {
		log.Error(err, "Cannot search")
		return false, err
	}

	// 3) Get the status
	status, err := wt.Status()
	if err != nil {
		log.Error(err, "Cannot get status")
		return false, err
	}

	// 4) Check for any changes
	if status.IsClean() {
		log.Info("No Changes to branch")
		if err = repo.Storer.RemoveReference(plumbing.ReferenceName("refs/heads/" + branch)); err != nil {
			log.Error(err, "Cannot remove reference")
			return false, err
		}
		if err = wt.Clean(&git.CleanOptions{
			Dir: true, // clean directories too
		}); err != nil {
			log.Error(err, "Cannot clean worktree")
			return false, err
		}
		return false, err
	} else {
		for path, f := range status {
			if f.Worktree != git.Unmodified {
				if _, err = wt.Add(path); err != nil {
					log.Error(err, "Cannot add path")
					return false, err
				}
			}
		}
		if _, err = wt.Commit("chore: update in-memory file", &git.CommitOptions{
			Author: &object.Signature{
				Name:  "CSVWatchdog Bot",
				Email: "csvwatchdogbot@test.com",
				When:  time.Now(),
			},
		}); err != nil {
			log.Error(err, "Cannot write")
			return false, err
		}

		// 6) Push back to origin
		if err = repo.Push(&git.PushOptions{
			Auth: auth,
			RefSpecs: []config.RefSpec{
				config.RefSpec("refs/heads/" + branch + ":refs/heads/" + branch),
			},
			Force: true,
		}); err != nil {
			log.Error(err, "Cannot push")
			return false, err
		}

		log.Info("Successfully Pushed changes to branch " + branch)
	}
	return true, nil
}

func searchFS(fs billy.Filesystem, root, query string) error {
	var walk func(path string) error

	walk = func(path string) error {
		infos, err := fs.ReadDir(path)
		if err != nil {
			return err
		}
		for _, info := range infos {
			fullPath := filepath.Join(path, info.Name())
			if info.IsDir() {
				if err := walk(fullPath); err != nil {
					return err
				}
			} else {
				if err = scanFileAndReplaceVersion(fs, fullPath, query); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := walk(root); err != nil {
		return err
	}
	return nil
}

// scanFile opens a file and returns any lines containing 'query', prefixed with path:line#
func scanFileAndReplaceVersion(fs billy.Filesystem, path, query string) error {
	f, err := fs.Open(path)
	if err != nil {
		log.Error(err, "Cannot Open File")
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			// handle or log the close error
			log.Error(err, "warning: failed to close file")
		}
	}()

	base := regexp.QuoteMeta(strings.Split(query, ".")[0])
	pattern := `(?m)^(\s*startingCSV:\s*)` + base + `.*$`
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}

	var buffer strings.Builder
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		buffer.WriteString(line)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if re.MatchString(line) {
			log.Info("Replacing the CSV version " + query)
			replacementTpl := `${1}` + query
			updated := re.ReplaceAllString(buffer.String(), replacementTpl)
			out, err := fs.OpenFile(path, os.O_RDWR|os.O_TRUNC|os.O_CREATE, 0644)
			if err != nil {
				return err
			}
			defer func() {
				if err := out.Close(); err != nil {
					// handle or log the close error
					log.Error(err, "warning: failed to close file")
				}
			}()
			if _, err := out.Write([]byte(updated)); err != nil {
				return err
			}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CSVWatchdogReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&botv1alpha1.CSVWatchdog{}).
		Named("csvwatchdog").
		Watches(&operatorsv1alpha1.InstallPlan{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
