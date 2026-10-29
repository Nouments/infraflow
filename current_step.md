# InfraFlow — Génération multi-plateforme : suivi du travail

## État final

- Date : 2026-10-29
- Branche : `develop`
- Commit de départ : `7f37903fd120fd4ccdf3ede3d408daecdfa33348` (`generation test`)
- Changements présents dans l’arbre de travail; aucun commit, push ou merge effectué.
- Résultat : validation/topologie renforcée, génération native intégrée à `generate-all`, et garde-fous d’isolation Agent/site ajoutés.

## Modifications intégrées

- Validation des identifiants, interfaces, liens, extrémités et adresses dupliquées/cohérentes, tout en conservant les formats YAML mono-site et multisite.
- Génération textuelle native pour les profils Cisco IOS-XE, MikroTik RouterOS 7 et FortiOS. Les chemins utilisent l’ID du périphérique, ou son nom en repli; le manifeste conserve les informations de cible lorsqu’elles sont fournies et marque la compatibilité `unknown` / `rendered_unverified`.
- Les artefacts natifs passent par `GenerateAll`, sont publiés dans le manifeste et sont couverts par des tests de chemin, de contenu et d’intégrité.
- Le Provider refuse les affectations Agent/site dupliquées. Si le catalogue contient des manifests de sites publiés, son démarrage vérifie qu’ils ont chacun un Agent configuré et rejette les credentials liés à un site absent. Les Agents scoped exigent et valident leur manifeste de site avant de traiter le catalogue.
- Documentation et fixture CLI ajustées pour refléter l’exigence du manifeste scoped.

## Décisions et limites

- Catalogue matériel : aucun téléchargement ni synchroniseur ajouté. L’entrée existante est une référence de page produit, sans asset précis ni droits de redistribution suffisamment établis.
- ZTP/AutoInstall : non implémentés. La recherche de conception n’est pas une preuve de compatibilité ni une validation en laboratoire.
- Génération native : rendu de texte seulement; aucune validation syntaxique constructeur, application sur équipement, ni matrice de compatibilité modèle/version. Le manifeste rend explicitement cette compatibilité inconnue. Les intentions non prises en charge sont rejetées.
- La couverture Agent/site est vérifiable pour les manifests publiés; un catalogue vide ne permet pas de prouver qu’il existe déjà un credential pour chaque site futur.
- Outils externes absents lors de l’audit initial : `ansible-playbook` et `terraform`. Aucun playbook mutateur, import RouterOS ou déploiement n’a été exécuté.

## Vérifications finales

- `go test -count=1 ./...` : réussi.
- `make fmt-check` : réussi.
- `make vet` : réussi.
- `make build` : réussi (`infraflow-provider` et `infraflow-agent`).
- `make test-scripts` : réussi, tous les tests de préflight Cisco passés.
- `git diff --check` : réussi.
