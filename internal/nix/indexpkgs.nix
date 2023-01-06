/* indexpkgs.nix is an expression that starts with the top-level attributes in
   nixpkgs and recursively walks the tree looking for derivations. The output is
   a flat attribute set containing package information keyed by package
   attribute paths.

   This expression doesn't descend into attribute sets that are missing the
   recurseForDerivations attribute. It also attempts to evaluate each
   derivation's outPath to ensure that the derivation is installable on the
   current system. Derivations that fail to evaluate are silently skipped.

   Debugging tip: put the following shebang at the top of this file and make it
   executable in order to run it with `./indexpkgs.nix | jq .`:

    #!/usr/bin/env nix eval --read-only --json --file
*/

with builtins;

let
  pkgs = import
    (fetchTarball {
      url = "https://github.com/nixos/nixpkgs/archive/{{ . }}.tar.gz";
    })
    { };

  homepageToStr = x:
    if isList x then head x
    else x;

  licenseToStr = x:
    if isList x then (head x).spdxId
    else if isAttrs x then x.spdxId
    else x;

  pkgInfo = pkg: {
    name = pkg.name;
    ${if pkg ? pname then "pname" else null} = pkg.pname;
    ${if pkg ? version then "version" else null} = pkg.version;
    ${if pkg ? nixpkgsVersion then "nixpkgs_version" else null} = pkg.nixpkgsVersion;
    ${if pkg ? meta.description then "description" else null} = pkg.meta.description;
    ${if pkg ? meta.longDescription then "long_description" else null} = pkg.meta.longDescription;
    ${if pkg ? meta.homepage then "homepage" else null} = homepageToStr pkg.meta.homepage;
    ${if pkg ? meta.license.spdxId then "license" else null} = licenseToStr pkg.meta.license;
    ${if pkg ? meta.platforms then "platforms" else null} = pkg.meta.platforms;
    ${if pkg ? meta.broken then "broken" else null} = pkg.meta.broken;
    ${if pkg ? meta.insecure then "insecure" else null} = pkg.meta.insecure;
  };

  walkPkgAttrs = attrPath: attrSet: foldl'
    (pkgIndex: attrName:
      let
        childPath = attrPath ++ [ attrName ];
        childAttrSet = attrSet."${attrName}";
        isDerivation = attrSet: (attrSet.type or null) == "derivation";
        result = tryEval
          (
            # Use `seq` to make sure that the derivation's store path can be
            # evaluated before building the attribute set. This ensures that
            # we only include packages than can be installed on the current
            # system.
            if isDerivation childAttrSet then
              seq childAttrSet.outPath { "${concatStringsSep "." childPath}" = (pkgInfo childAttrSet); }
            else if childAttrSet.recurseForDerivations or false then
              walkPkgAttrs childPath childAttrSet
            else
              { }
          );
      in
      if result.success then pkgIndex // result.value else pkgIndex
    )
    { }
    (attrNames attrSet);
in
walkPkgAttrs [ ] pkgs
