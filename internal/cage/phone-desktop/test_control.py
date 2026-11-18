"""Regression checks for the phone window geometry and host mount specification."""
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch

from Xlib import Xutil
import control
from view import View, fitted_window


class PhoneWindowTests(unittest.TestCase):
    def test_viewport_matches_electron_dpi_even_while_typing(self):
        for size, expected in [((390, 672), (585, 1008)), ((672, 390), (1008, 585)),
                               ((390, 242), (585, 363))]:
            view = View.__new__(View)
            view.root = Mock()
            view.root.get_geometry.return_value = SimpleNamespace(width=200, height=160)
            view.values = lambda *_: []
            view.scale = 1.5
            view.dynamic_scale = False
            with tempfile.TemporaryDirectory() as tmp, patch('view.layout_file', Path(tmp)/'layout'), \
                    patch('builtins.open', return_value=tempfile.TemporaryFile('w+')), patch('view.display_size') as resize:
                view.resize(*size)
            self.assertEqual(view.pixels, expected)
            resize.assert_called_once_with(*expected)

    def test_only_verified_maximized_window_relaxes_desktop_minimum(self):
        # A mobile display of 585px starts with the actual Claude Electron
        # minimum 900x600, which Openbox honors until this hint is lowered.
        from view import Failure
        view = View.__new__(View)
        view.size = (390, 672)
        view.root = Mock()
        view.conn = Mock()
        view.conn.intern_atom.side_effect = lambda name: name
        view.values = lambda w, k: [12] if w is view.root or k == '_NET_WM_PID' else [
            '_NET_WM_STATE_MAXIMIZED_VERT', '_NET_WM_STATE_MAXIMIZED_HORZ']
        view.pixels = (585, 1008)
        real = Mock()
        real.get_geometry.return_value = SimpleNamespace(width=900, height=1008)
        hints = dict(flags=Xutil.PMinSize, min_width=900, min_height=600, win_gravity=10)
        real.get_wm_normal_hints.return_value = SimpleNamespace(**hints, _data=hints)
        real.get_wm_transient_for.return_value = None
        view.window = lambda _: real
        view.role = lambda _: ''
        with self.assertRaises(Failure):
            view.active()
        real.set_wm_normal_hints.assert_not_called()
        view.role = lambda _: 'desktop'
        with tempfile.TemporaryDirectory() as tmp, patch('view.layout_file', Path(tmp)/'layout'):
            import view as desktop
            desktop.save_layout({'minimums':{}})
            with self.assertRaises(Failure):
                view.active()
        real.set_wm_normal_hints.assert_called_once_with(**{**hints, 'min_width': 585})

    def test_nonmaximized_dialog_uses_matching_picture_and_input_geometry(self):
        ratio, left, top = fitted_window((390, 672), SimpleNamespace(width=600, height=300))
        self.assertEqual((150 * ratio + left, 150 * ratio + top), (97.5, 336))

    def test_phone_attaches_to_existing_session_with_read_only_home(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(Path, 'is_file', return_value=True), \
                patch.object(Path, 'exists', return_value=True), patch.object(control, 'unit_state', return_value=b'inactive'), \
                patch.object(control, 'command', return_value=b'active') as cmd, patch.object(control, 'stop'), \
                patch.object(control.pwd, 'getpwnam', return_value=SimpleNamespace(pw_dir=tmp,pw_name='user',pw_gid=99)):
            record = Path(tmp)/'owner'
            record.write_text('{}')
            control.launch({'id':'phone','width':390,'height':672}, record)
            self.assertEqual(json.loads(record.read_text())['mode'], 'shared')
            args = cmd.call_args.args
            self.assertIn('--property=ProtectHome=read-only', args)
            self.assertIn('--property=BindsTo=cage-claude-desktop.service', args)
            self.assertFalse(any('BindPaths=' in arg for arg in args))


if __name__ == '__main__':
    unittest.main()
