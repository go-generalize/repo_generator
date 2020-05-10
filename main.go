package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fatih/structtag"
	"github.com/iancoleman/strcase"
	"golang.org/x/xerrors"
)

func main() {
	l := len(os.Args)
	if l < 2 {
		fmt.Println("You have to specify the struct name of target")
		os.Exit(1)
	}

	if err := run(os.Args[1]); err != nil {
		log.Fatal(err.Error())
	}
}

func run(structName string) error {
	fs := token.NewFileSet()
	pkgs, err := parser.ParseDir(fs, ".", nil, parser.AllErrors)

	if err != nil {
		panic(err)
	}

	for name, v := range pkgs {
		if strings.HasSuffix(name, "_test") {
			continue
		}

		return traverse(v, fs, structName)
	}

	return nil
}

func traverse(pkg *ast.Package, fs *token.FileSet, structName string) error {
	gen := &generator{PackageName: pkg.Name}
	for name, file := range pkg.Files {
		gen.FileName = strings.TrimSuffix(filepath.Base(name), ".go")
		gen.GeneratedFileName = gen.FileName + "_gen"

		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			if genDecl.Tok != token.TYPE {
				continue
			}

			for _, spec := range genDecl.Specs {
				// 型定義
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				name := typeSpec.Name.Name

				if name != structName {
					continue
				}

				// structの定義
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				gen.StructName = name

				return generate(gen, fs, structType)
			}
		}
	}

	return xerrors.Errorf("no such struct: %s", structName)
}

func uppercaseExtraction(name string) (lower string) {
	for _, x := range name {
		if 65 <= x && x <= 90 {
			lower += string(x + 32)
		}
	}
	return
}

const (
	biunigrams  = "Biunigrams"
	prefix      = "Prefix"
	queryLabel  = "QueryLabel"
	typeString  = "string"
	typeInt     = "int"
	typeInt64   = "int64"
	typeFloat64 = "float64"
)

var valueCheck = regexp.MustCompile("^[0-9a-zA-Z_]+$")

func generate(gen *generator, fs *token.FileSet, structType *ast.StructType) error {
	dupMap := make(map[string]int)
	f := func(name string) string {
		u := uppercaseExtraction(name)
		if _, ok := dupMap[u]; !ok {
			dupMap[u] = 1
		} else {
			dupMap[u]++
			u = fmt.Sprintf("%s%d", u, dupMap[u])
		}
		return u
	}
	fieldLabel := gen.StructName + queryLabel
	appendIndexesInfo := func(fieldInfo *FieldInfo) {
		idx := &IndexesInfo{
			ConstName: fieldLabel + fieldInfo.Field,
			Label:     f(fieldInfo.Field),
			Method:    "Add",
		}
		idx.Comment = fmt.Sprintf("%s %s", idx.ConstName, fieldInfo.Field)
		if fieldInfo.FieldType != typeString {
			idx.Method += "Something"
		}
		fieldInfo.Indexes = append(fieldInfo.Indexes, idx)
	}
	for _, field := range structType.Fields.List {
		// structの各fieldを調査
		if len(field.Names) != 1 {
			return xerrors.New("`field.Names` must have only one element")
		}
		name := field.Names[0].Name

		typeName := getTypeName(field.Type)

		if field.Tag == nil {
			fieldInfo := &FieldInfo{
				DsTag:     name,
				Field:     name,
				FieldType: typeName,
				Indexes:   make([]*IndexesInfo, 0),
			}
			appendIndexesInfo(fieldInfo)
			gen.FieldInfos = append(gen.FieldInfos, fieldInfo)
			continue
		}

		pos := fs.Position(field.Pos()).String()

		if tags, err := structtag.Parse(strings.Trim(field.Tag.Value, "`")); err != nil {
			log.Printf(
				"%s: tag for %s in struct %s in %s",
				pos, name, gen.StructName, gen.GeneratedFileName+".go",
			)
			continue
		} else {
			if name == "Indexes" {
				gen.EnableIndexes = true
				fieldInfo := &FieldInfo{
					DsTag:     name,
					Field:     name,
					FieldType: typeName,
				}
				if tag, err := tagCheck(pos, tags); err != nil {
					return xerrors.Errorf("error in tagCheck method: %w", err)
				} else if tag != "" {
					fieldInfo.DsTag = tag
				}
				gen.FieldInfoForIndexes = fieldInfo
				continue
			}
			if _, err := tags.Get("datastore_key"); err != nil {
				fieldInfo := &FieldInfo{
					DsTag:     name,
					Field:     name,
					FieldType: typeName,
					Indexes:   make([]*IndexesInfo, 0),
				}
				if tag, err := tagCheck(pos, tags); err != nil {
					return xerrors.Errorf("error in tagCheck method: %w", err)
				} else if tag != "" {
					fieldInfo.DsTag = tag
				}

				if idr, err := tags.Get("indexer"); err != nil || fieldInfo.FieldType != typeString {
					appendIndexesInfo(fieldInfo)
				} else {
					filters := strings.Split(idr.Value(), ",")
					dupIdr := make(map[string]struct{})
					for _, fil := range filters {
						idx := &IndexesInfo{
							ConstName: fieldLabel + name,
							Label:     f(name),
							Method:    "Add",
						}
						var dupFlag string
						switch fil {
						case "p", "prefix": // 前方一致 (AddPrefix)
							idx.Method += prefix
							idx.ConstName += prefix
							idx.Comment = fmt.Sprintf("%s %s前方一致", idx.ConstName, name)
							dupFlag = "p"
						case "s", "suffix": /* TODO 後方一致
							idx.Method += Suffix
							idx.ConstName += Suffix
							idx.Comment = fmt.Sprintf("%s %s後方一致", idx.ConstName, name)
							dup = "s"*/
						case "e", "equal": // 完全一致 (Add) Default
							idx.Comment = fmt.Sprintf("%s %s", idx.ConstName, name)
							dupIdr["equal"] = struct{}{}
							dupFlag = "e"
						case "l", "like": // 部分一致
							idx.Method += biunigrams
							idx.ConstName += "Like"
							idx.Comment = fmt.Sprintf("%s %s部分一致", idx.ConstName, name)
							dupFlag = "l"
						default:
							continue
						}
						if _, ok := dupIdr[dupFlag]; ok {
							continue
						}
						dupIdr[dupFlag] = struct{}{}
						fieldInfo.Indexes = append(fieldInfo.Indexes, idx)
					}
				}

				gen.FieldInfos = append(gen.FieldInfos, fieldInfo)
				continue
			}

			dsTag, err := tags.Get("datastore")

			// datastore タグが存在しないか-になっていない
			if err != nil || strings.Split(dsTag.Value(), ",")[0] != "-" {
				return xerrors.Errorf("%s: key field for datastore should have datastore:\"-\" tag", pos)
			}

			gen.KeyFieldName = name
			gen.KeyFieldType = typeName

			if gen.KeyFieldType != typeInt64 &&
				gen.KeyFieldType != typeString &&
				!strings.HasSuffix(gen.KeyFieldType, ".Key") {
				return xerrors.Errorf("%s: supported key types are int64, string, *datastore.Key", pos)
			}

			gen.KeyValueName = strcase.ToLowerCamel(name)
		}
	}

	{
		fp, err := os.Create(gen.GeneratedFileName + ".go")
		if err != nil {
			panic(err)
		}
		defer fp.Close()

		gen.generate(fp)
	}

	if gen.EnableIndexes {
		path := gen.FileName + "_label.go"
		fp, err := os.Create(path)
		if err != nil {
			panic(err)
		}
		defer fp.Close()
		gen.generateLabel(fp)
	}

	{
		fp, err := os.Create("constant.go")
		if err != nil {
			panic(err)
		}
		defer fp.Close()
		gen.generateConstant(fp)
	}

	return nil
}

func tagCheck(pos string, tags *structtag.Tags) (string, error) {
	if dsTag, err := tags.Get("datastore"); err == nil {
		tag := strings.Split(dsTag.Value(), ",")[0]
		if !valueCheck.MatchString(tag) {
			return "", xerrors.Errorf("%s: key field for datastore should have other than blanks and symbols tag", pos)
		}
		if strings.Contains("0123456789", string(tag[0])) {
			return "", xerrors.Errorf("%s: key field for datastore should have prefix other than numbers required", pos)
		}
		return tag, nil
	}
	return "", nil
}
