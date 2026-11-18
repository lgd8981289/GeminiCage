import os
from pathlib import Path
from unittest import TestCase
from unittest.mock import Mock, patch
from Xlib import X
import control
import view

APP = '/opt/google-antigravity/current/antigravity'

class AntigravityPhoneTests(TestCase):
    def test_shared_desktop_detects_the_installed_app(self):
        with patch.object(Path, 'is_file', lambda p: str(p) == APP):
            with self.assertRaises(control.Failure) as error:
                control.launch({'id':'test','width':0,'height':0}, Path('unused'))
            self.assertEqual(error.exception.status, 400)

    def test_only_the_current_antigravity_executable_is_selected(self):
        display = view.View.__new__(view.View)
        display.values = lambda *_: [52]
        win = Mock()
        win.get_attributes.return_value.map_state = X.IsViewable
        win.get_wm_class.return_value = ('antigravity', 'Antigravity')
        with patch.object(Path, 'stat', return_value=Mock(st_uid=os.getuid())), patch.object(os, 'readlink', return_value='/opt/google-antigravity/release-test/antigravity'), patch.object(os.path, 'samefile', return_value=True):
            self.assertEqual(display.role(win), 'desktop')
            with patch.object(os.path, 'samefile', return_value=False):
                self.assertEqual(display.role(win), '')
