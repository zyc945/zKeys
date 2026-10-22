import contextlib
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import zwork

REAL_RUN = subprocess.run

class Tests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='zwork-project-test-')
        self.root = Path(self.tmp.name).resolve()
        self.previous = Path.cwd()
        os.chdir(self.root)
        self.output = io.StringIO()
        self.stderr = contextlib.redirect_stderr(self.output)
        self.stderr.__enter__()
    def tearDown(self):
        self.stderr.__exit__(None,None,None)
        os.chdir(self.previous)
        self.tmp.cleanup()
    def config(self, value=None, raw=None):
        (self.root/'.zkey.json').write_text(raw if raw is not None else json.dumps(value or {'env':{'API_KEY':'work/api'}}))
    def call(self, argv, listing='', status=0, error=''):
        def run(args, **kwargs):
            if args[0]=='git': return REAL_RUN(args, **kwargs)
            return subprocess.CompletedProcess(args,status,listing,error)
        with patch.object(zwork.subprocess,'run',side_effect=run),patch.object(zwork.os,'execv') as execute:
            zwork.main(argv)
            return execute.call_args.args
    def fail(self,argv,**kwargs):
        with self.assertRaises(SystemExit) as result: self.call(argv,**kwargs)
        self.assertEqual(result.exception.code,2)
    def test_non_git_project_config(self):
        self.config()
        command,args=self.call(['work'])
        self.assertTrue(command.endswith('/zkey'))
        self.assertEqual(args[1:4],['run','--env','API_KEY=work/api'])
        self.assertIn('API_KEY <- work/api',self.output.getvalue())
    def test_git_root_from_nested_directory(self):
        REAL_RUN(['git','init','-q',str(self.root)],check=True)
        self.config()
        sub=self.root/'src'; sub.mkdir(); (sub/'.zkey.json').write_text('invalid')
        os.chdir(sub)
        _,args=self.call(['work'])
        self.assertIn('API_KEY=work/api',args)
        self.assertEqual(Path.cwd(),sub)
    def test_non_git_does_not_search_parent(self):
        self.config(); sub=self.root/'sub'; sub.mkdir(); os.chdir(sub)
        self.fail(['work'])
    def test_explicit_keys_ignore_broken_config(self):
        self.config(raw='invalid')
        with patch.object(zwork,'project_keys',side_effect=AssertionError('config accessed')):
            _,args=self.call(['work','TOKEN','OTHER=work/other'])
        self.assertEqual(args[1:6],['run','--env','TOKEN=TOKEN','--env','OTHER=work/other'])
    def test_cli_does_not_merge_config(self):
        self.config()
        _,args=self.call(['work','OTHER'])
        self.assertNotIn('API_KEY=work/api',args)
    def test_existing_attach_skips_config(self):
        with patch.object(zwork,'project_keys',side_effect=AssertionError('config accessed')):
            _,args=self.call(['work'],listing='work [Created 1m ago]\n')
        self.assertEqual(args[1:],['attach','work'])
    def test_existing_rejects_explicit_keys(self):
        self.fail(['work','TOKEN'],listing='work [Created 1m ago]\n')
    def test_dead_rejected(self):
        self.fail(['work'],listing='work [Created 1m ago] (EXITED - attach to resurrect)\n')
    def test_active_exited_name(self):
        _,args=self.call(['work-EXITED'],listing='work-EXITED [Created 1m ago]\n')
        self.assertEqual(args[1:],['attach','work-EXITED'])
    def test_no_sessions_status(self):
        self.config()
        _,args=self.call(['work'],status=1,error='No active zellij sessions found.')
        self.assertIn('API_KEY=work/api',args)
    def test_list_failure_stops(self):
        self.config(); self.fail(['work'],status=1,error='permission failure')
    def test_missing_config(self): self.fail(['work'])
    def test_bad_configs(self):
        for value in [[],{}, {'env':{}},{'env':[]},{'env':{'KEY':None}}, {'env':{'KEY':3}}, {'env':{'KEY':'x'},'other':True},{'env':{'BAD NAME':'x'}},{'env':{'KEY':'bad value'}},{'env':{'KEY':''}}]:
            with self.subTest(value=value):
                self.config(raw=json.dumps(value)); self.fail(['work'])
    def test_duplicate_json_keys_rejected(self):
        for raw in ['{"env":{"KEY":"x","KEY":"y"}}','{"env":{"KEY":"x"},"env":{"KEY":"y"}}']:
            self.config(raw=raw); self.fail(['work'])
    def test_invalid_json_not_echoed(self):
        self.config(raw='fake-sensitive-invalid-content')
        self.fail(['work'])
        self.assertNotIn('fake-sensitive-invalid-content',self.output.getvalue())
    def test_invalid_cli_names(self):
        for argv in [['bad name','KEY'],['work','A=x','A=y'],['work','1BAD']]: self.fail(argv)
    def test_real_process_handoff(self):
        home=self.root/'home'; bindir=home/'.local/bin'; bindir.mkdir(parents=True)
        zellij=bindir/'zellij'; zellij.write_text('#!/bin/sh\nexit 0\n'); zellij.chmod(0o700)
        zkey=bindir/'zkey'
        zkey.write_text('#!/usr/bin/python3\nimport json,sys\nfrom pathlib import Path\nPath("received.json").write_text(json.dumps(sys.argv[1:]))\n')
        zkey.chmod(0o700)
        self.config()
        env={**os.environ,'HOME':str(home)}
        result=REAL_RUN(['/usr/bin/python3',zwork.__file__,'work'],env=env,capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(json.loads((self.root/'received.json').read_text()),['run','--env','API_KEY=work/api','--',str(zellij),'--session','work'])

if __name__=='__main__': unittest.main()
