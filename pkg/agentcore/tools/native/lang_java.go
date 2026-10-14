package native

import (
	"os"
	"path/filepath"
	"strings"
)

// javaLanguage is the Java declaration: the local Maven repository and the
// Gradle module cache as read-only roots, `javap` for a dependency's API, and
// the Maven and Gradle commands that run the tests.
//
// The test commands cover the two wrappers as well (suga-iizi): `./mvnw` and
// `mvnw.cmd` behave like `mvn`, `gradlew`/`gradlew.bat` like `gradle`, and a
// Gradle task of a subproject (`./gradlew :app:test`) runs the tests of that
// module. `mvn verify` runs the tests too (the integration ones as well), so it
// counts like `mvn test`. A run limited to some tests is `-Dtest=` for Maven
// and `--tests` for Gradle; both are declared as the flag without its "=",
// which the shared pattern's trailing "(\s|=)" accepts.
var javaLanguage = Language{
	Name:    "Java",
	Markers: []string{"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"},
	DependencyAPI: "for Java, `javap -public -cp <jar> <fully.qualified.Class>` with the jar under " +
		"~/.m2/repository/<group path>/<artifact>/<version>/ (`mvn -o dependency:sources` fetches the sources)",
	TestCommands: `\bmvnw?(\.cmd)?( \S+)* (test|verify)\b` +
		`|\bgradlew?(\.bat)?( \S+)* (:[A-Za-z0-9_.-]+:)?test\b`,
	FocusedFlags: []string{"-Dtest", "--tests"},
	dependencyRoots: func(env langEnv, add *rootCollector) {
		// The local Maven repository (a settings.xml may move it elsewhere) and
		// the Gradle module cache hold the jars and poms, addressed by groupId
		// path: javap and the file tools work straight off these paths.
		if env.home != "" {
			repo := readMavenLocalRepository(filepath.Join(env.home, ".m2", "settings.xml"), env.home)
			if repo == "" {
				repo = filepath.Join(env.home, ".m2", "repository")
			}
			add.add("Maven local repository", repo)
		}
		gradleHome := os.Getenv("GRADLE_USER_HOME")
		if gradleHome == "" && env.home != "" {
			gradleHome = filepath.Join(env.home, ".gradle")
		}
		if gradleHome != "" {
			add.add("Gradle module cache", filepath.Join(gradleHome, "caches", "modules-2", "files-2.1"))
		}
	},
}

// readMavenLocalRepository returns the <localRepository> of a Maven
// settings.xml (the one place the local repository is configured), with
// ${user.home} resolved; a commented-out element does not count. "" when the
// file is missing or holds no usable value.
func readMavenLocalRepository(path, home string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := stripXMLComments(string(data))
	start := strings.Index(text, "<localRepository>")
	if start < 0 {
		return ""
	}
	rest := text[start+len("<localRepository>"):]
	end := strings.Index(rest, "</localRepository>")
	if end < 0 {
		return ""
	}
	value := strings.TrimSpace(rest[:end])
	if home != "" {
		value = strings.ReplaceAll(value, "${user.home}", home)
	}
	return value
}

// stripXMLComments removes every <!-- ... --> section; an unclosed comment
// swallows the rest of the text.
func stripXMLComments(s string) string {
	for {
		start := strings.Index(s, "<!--")
		if start < 0 {
			return s
		}
		end := strings.Index(s[start+len("<!--"):], "-->")
		if end < 0 {
			return s[:start]
		}
		s = s[:start] + s[start+len("<!--")+end+len("-->"):]
	}
}
