package panel

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxEditableFileSize = 1 << 20
	maxUploadFileSize   = 32 << 20
)

type FileEntry struct {
	Name    string
	Path    string
	IsDir   bool
	Size    int64
	ModTime string
}

type FileView struct {
	RootLabel   string
	CurrentPath string
	ParentPath  string
	Entries     []FileEntry
	FileName    string
	FilePath    string
	FileContent string
	IsEditing   bool
}

func (a *App) handleFiles(w http.ResponseWriter, r *http.Request) {
	domains, err := a.fileDomains(r)
	if err != nil {
		http.Error(w, "could not list domains", http.StatusInternalServerError)
		return
	}
	a.render(w, http.StatusOK, "files", pageData{
		Session:     sessionFromContext(r.Context()),
		FileDomains: domains,
	})
}

func (a *App) handleFilesDomain(w http.ResponseWriter, r *http.Request) {
	domain, root, ok := a.fileDomainAndRoot(w, r)
	if !ok {
		return
	}
	a.renderFileView(w, r, http.StatusOK, domain, root, "", "")
}

func (a *App) handleFileSave(w http.ResponseWriter, r *http.Request) {
	domain, root, ok := a.fileDomainAndRoot(w, r)
	if !ok {
		return
	}
	rel, abs, err := safeManagedPath(root, r.FormValue("path"))
	if err != nil || rel == "." {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "Path file tidak valid.", "")
		return
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "File tidak ditemukan.", "")
		return
	}
	content := r.FormValue("content")
	if len(content) > maxEditableFileSize {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "File terlalu besar untuk diedit dari panel.", "")
		return
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		a.renderFileView(w, r, http.StatusInternalServerError, domain, root, "Gagal menyimpan file.", "")
		return
	}
	http.Redirect(w, r, "/files/"+strconv.FormatInt(domain.ID, 10)+"?path="+rel, http.StatusSeeOther)
}

func (a *App) handleFileMkdir(w http.ResponseWriter, r *http.Request) {
	domain, root, ok := a.fileDomainAndRoot(w, r)
	if !ok {
		return
	}
	current, absCurrent, err := safeManagedPath(root, r.FormValue("path"))
	if err != nil {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "Path folder tidak valid.", "")
		return
	}
	name, err := normalizeFileName(r.FormValue("name"))
	if err != nil {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "Nama folder tidak valid.", "")
		return
	}
	target := filepath.Join(absCurrent, name)
	if _, _, err := safeManagedPath(root, joinRelPath(current, name)); err != nil {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "Nama folder tidak valid.", "")
		return
	}
	if err := os.Mkdir(target, 0755); err != nil {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "Gagal membuat folder.", "")
		return
	}
	http.Redirect(w, r, "/files/"+strconv.FormatInt(domain.ID, 10)+"?path="+current, http.StatusSeeOther)
}

func (a *App) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	domain, root, ok := a.fileDomainAndRoot(w, r)
	if !ok {
		return
	}
	current, _, err := safeManagedPath(root, r.FormValue("path"))
	if err != nil {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "Path upload tidak valid.", "")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "File wajib diupload.", "")
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > maxUploadFileSize {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "Ukuran file maksimal 32 MB.", "")
		return
	}
	name, err := normalizeFileName(header.Filename)
	if err != nil {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "Nama file tidak valid.", "")
		return
	}
	targetRel := joinRelPath(current, name)
	_, target, err := safeManagedPath(root, targetRel)
	if err != nil {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "Nama file tidak valid.", "")
		return
	}
	if _, err := os.Stat(target); err == nil {
		a.renderFileView(w, r, http.StatusConflict, domain, root, "File sudah ada.", "")
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		a.renderFileView(w, r, http.StatusInternalServerError, domain, root, "Gagal mengecek file.", "")
		return
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		a.renderFileView(w, r, http.StatusInternalServerError, domain, root, "Gagal menyimpan upload.", "")
		return
	}
	if _, err := io.Copy(out, io.LimitReader(file, maxUploadFileSize+1)); err != nil {
		out.Close()
		_ = os.Remove(target)
		a.renderFileView(w, r, http.StatusInternalServerError, domain, root, "Gagal menyimpan upload.", "")
		return
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(target)
		a.renderFileView(w, r, http.StatusInternalServerError, domain, root, "Gagal menyimpan upload.", "")
		return
	}
	http.Redirect(w, r, "/files/"+strconv.FormatInt(domain.ID, 10)+"?path="+current, http.StatusSeeOther)
}

func (a *App) handleFileDelete(w http.ResponseWriter, r *http.Request) {
	domain, root, ok := a.fileDomainAndRoot(w, r)
	if !ok {
		return
	}
	rel, abs, err := safeManagedPath(root, r.FormValue("path"))
	if err != nil || rel == "." {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "Path hapus tidak valid.", "")
		return
	}
	if err := os.Remove(abs); err != nil {
		a.renderFileView(w, r, http.StatusBadRequest, domain, root, "Gagal menghapus. Folder harus kosong.", "")
		return
	}
	parent := parentRelPath(rel)
	http.Redirect(w, r, "/files/"+strconv.FormatInt(domain.ID, 10)+"?path="+parent, http.StatusSeeOther)
}

func (a *App) renderFileView(w http.ResponseWriter, r *http.Request, status int, domain *Domain, root, errMsg, okMsg string) {
	domains, err := a.fileDomains(r)
	if err != nil {
		http.Error(w, "could not list domains", http.StatusInternalServerError)
		return
	}
	view, err := a.buildFileView(root, r.URL.Query().Get("path"))
	if err != nil {
		view = FileView{RootLabel: filepath.ToSlash(root), CurrentPath: "."}
		if errMsg == "" {
			errMsg = err.Error()
		}
	}
	a.render(w, status, "files", pageData{
		Session:        sessionFromContext(r.Context()),
		FileDomains:    domains,
		SelectedDomain: domain,
		FileView:       view,
		Error:          errMsg,
		Notice:         okMsg,
	})
}

func (a *App) buildFileView(root, requested string) (FileView, error) {
	rel, abs, err := safeManagedPath(root, requested)
	if err != nil {
		return FileView{}, errors.New("path tidak valid")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return FileView{}, errors.New("path tidak ditemukan")
	}
	view := FileView{
		RootLabel:   filepath.ToSlash(root),
		CurrentPath: rel,
		ParentPath:  parentRelPath(rel),
	}
	if info.IsDir() {
		entries, err := os.ReadDir(abs)
		if err != nil {
			return FileView{}, errors.New("folder tidak bisa dibaca")
		}
		for _, entry := range entries {
			entryInfo, err := entry.Info()
			if err != nil {
				continue
			}
			view.Entries = append(view.Entries, FileEntry{
				Name:    entry.Name(),
				Path:    joinRelPath(rel, entry.Name()),
				IsDir:   entry.IsDir(),
				Size:    entryInfo.Size(),
				ModTime: entryInfo.ModTime().Format("2006-01-02 15:04"),
			})
		}
		sort.Slice(view.Entries, func(i, j int) bool {
			if view.Entries[i].IsDir != view.Entries[j].IsDir {
				return view.Entries[i].IsDir
			}
			return strings.ToLower(view.Entries[i].Name) < strings.ToLower(view.Entries[j].Name)
		})
		return view, nil
	}
	if info.Size() > maxEditableFileSize {
		return FileView{}, errors.New("file terlalu besar untuk diedit dari panel")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return FileView{}, errors.New("file tidak bisa dibaca")
	}
	if !utf8.Valid(data) {
		return FileView{}, errors.New("file binary tidak bisa diedit dari panel")
	}
	view.IsEditing = true
	view.FileName = filepath.Base(abs)
	view.FilePath = rel
	view.FileContent = string(data)
	return view, nil
}

func (a *App) fileDomainAndRoot(w http.ResponseWriter, r *http.Request) (*Domain, string, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return nil, "", false
	}
	domain, err := a.store.GetDomain(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return nil, "", false
	}
	root, ok := a.fileRootForDomain(domain)
	if !ok {
		http.Error(w, "domain ini tidak punya folder aplikasi yang dikelola panel", http.StatusBadRequest)
		return nil, "", false
	}
	return domain, root, true
}

func (a *App) fileDomains(r *http.Request) ([]Domain, error) {
	domains, err := a.store.ListDomains(r.Context())
	if err != nil {
		return nil, err
	}
	filtered := domains[:0]
	for _, domain := range domains {
		if _, ok := a.fileRootForDomain(&domain); ok {
			filtered = append(filtered, domain)
		}
	}
	return filtered, nil
}

func (a *App) fileRootForDomain(domain *Domain) (string, bool) {
	switch domain.SiteType {
	case "go_binary":
		return filepath.Join(a.appRoot, domain.Domain), true
	case "node_next", "php_git":
		return filepath.Join(a.appRoot, domain.Domain, "source"), true
	case "static", "php":
		return filepath.Join(a.appRoot, domain.Domain, "public"), true
	default:
		return "", false
	}
}

func safeManagedPath(root, requested string) (string, string, error) {
	root = filepath.Clean(root)
	cleaned := cleanRelPath(requested)
	if cleaned == "" {
		cleaned = "."
	}
	if filepath.IsAbs(cleaned) || filepath.VolumeName(cleaned) != "" {
		return "", "", ErrInvalidPath
	}
	for _, part := range strings.Split(cleaned, string(os.PathSeparator)) {
		if part == ".." {
			return "", "", ErrInvalidPath
		}
	}
	target := filepath.Clean(filepath.Join(root, cleaned))
	relToRoot, err := filepath.Rel(root, target)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(os.PathSeparator)) || filepath.IsAbs(relToRoot) {
		return "", "", ErrInvalidPath
	}
	if relToRoot == "." {
		return ".", root, nil
	}
	return filepath.ToSlash(relToRoot), target, nil
}

func cleanRelPath(input string) string {
	value := strings.TrimSpace(input)
	if value == "" || value == "." {
		return "."
	}
	value = strings.ReplaceAll(value, "\\", "/")
	return filepath.Clean(filepath.FromSlash(value))
}

func normalizeFileName(input string) (string, error) {
	name := strings.TrimSpace(filepath.Base(strings.ReplaceAll(input, "\\", "/")))
	if len(name) == 0 || len(name) > 120 || name == "." || name == ".." || strings.ContainsRune(name, 0) {
		return "", ErrInvalidPath
	}
	if strings.ContainsAny(name, `/\`) {
		return "", ErrInvalidPath
	}
	return name, nil
}

func joinRelPath(base, name string) string {
	if base == "" || base == "." {
		return name
	}
	return path.Clean(filepath.ToSlash(base) + "/" + name)
}

func parentRelPath(rel string) string {
	rel = filepath.ToSlash(cleanRelPath(rel))
	if rel == "." || rel == "" {
		return "."
	}
	parent := path.Dir(rel)
	if parent == "." || parent == "/" {
		return "."
	}
	return parent
}
