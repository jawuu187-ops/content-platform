# Keep line numbers so crash reports stay actionable, but hide the source file
# name. Kotlin metadata is kept for reflection used by Compose tooling only in
# debug builds.
-keepattributes SourceFile,LineNumberTable
-renamesourcefileattribute SourceFile
