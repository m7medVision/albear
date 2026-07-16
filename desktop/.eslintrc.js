module.exports = {
  extends: 'erb',
  plugins: ['@typescript-eslint'],
  rules: {
    // A temporary hack related to IDE not resolving correct package.json
    'import/no-extraneous-dependencies': 'off',
    'react/react-in-jsx-scope': 'off',
    'react/jsx-filename-extension': 'off',
    'import/extensions': 'off',
    'import/no-unresolved': 'off',
    'import/no-import-module-exports': 'off',
    'no-shadow': 'off',
    '@typescript-eslint/no-shadow': 'error',
    'no-unused-vars': 'off',
    '@typescript-eslint/no-unused-vars': 'error',
    // TypeScript checks props; PropTypes/defaultProps are legacy (React 19
    // dropped defaultProps for function components).
    'react/prop-types': 'off',
    'react/require-default-props': 'off',
    // shadcn-style UI primitives forward rest props by design.
    'react/jsx-props-no-spreading': 'off',
    // Named exports are used consistently across the codebase.
    'import/prefer-default-export': 'off',
    // noise.ts (Noise protocol port kept in sync with the extension) and the
    // vault client group small cohesive classes per module.
    'max-classes-per-file': 'off',
    // Crypto code: bitwise ops and counter increments are intentional.
    'no-bitwise': 'off',
    'no-plusplus': 'off',
    // `void promise` marks intentionally un-awaited promises (also used in
    // JSX expression position, so allowAsStatement would not suffice).
    'no-void': 'off',
    // Electron ships a modern V8: for..of needs no regenerator-runtime.
    // Keep the remaining airbnb restrictions.
    'no-restricted-syntax': [
      'error',
      {
        selector: 'ForInStatement',
        message:
          'for..in iterates over the prototype chain; use Object.{keys,values,entries} instead.',
      },
      {
        selector: 'LabeledStatement',
        message: 'Labels are a form of GOTO; avoid them.',
      },
      {
        selector: 'WithStatement',
        message: '`with` is disallowed in strict mode.',
      },
    ],
  },
  parserOptions: {
    ecmaVersion: 2022,
    sourceType: 'module',
  },
  settings: {
    'import/resolver': {
      // See https://github.com/benmosher/eslint-plugin-import/issues/1396#issuecomment-575727774 for line below
      node: {
        extensions: ['.js', '.jsx', '.ts', '.tsx'],
        moduleDirectory: ['node_modules', 'src/'],
      },
      webpack: {
        config: require.resolve('./.erb/configs/webpack.config.eslint.ts'),
      },
      typescript: {},
    },
    'import/parsers': {
      '@typescript-eslint/parser': ['.ts', '.tsx'],
    },
  },
};
